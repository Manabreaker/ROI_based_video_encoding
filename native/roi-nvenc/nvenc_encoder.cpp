#include "nvenc_encoder.h"

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#else
#include <dlfcn.h>
#endif

#include <algorithm>
#include <array>
#include <cerrno>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <fstream>
#include <iostream>
#include <memory>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

#include "../../third_party/nvidia-video-codec-sdk/include/nvEncodeAPI.h"

namespace roi_nvenc {

namespace {

using CUdevice = int;
using CUcontext = void*;
using CUresult = int;

constexpr CUresult CUDA_SUCCESS_VALUE = 0;

using CuInitFn = CUresult (*)(unsigned int);
using CuDeviceGetCountFn = CUresult (*)(int*);
using CuDeviceGetFn = CUresult (*)(CUdevice*, int);
using CuCtxCreateFn = CUresult (*)(CUcontext*, unsigned int, CUdevice);
using CuCtxDestroyFn = CUresult (*)(CUcontext);

using NvEncodeAPICreateInstanceFn = NVENCSTATUS(NVENCAPI *)(NV_ENCODE_API_FUNCTION_LIST*);

#ifdef _WIN32
std::string LastWindowsError() {
  DWORD err = GetLastError();
  if (err == 0) {
    return "unknown Windows error";
  }

  char* message = nullptr;
  DWORD len = FormatMessageA(FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM |
                                 FORMAT_MESSAGE_IGNORE_INSERTS,
                             nullptr, err, MAKELANGID(LANG_NEUTRAL, SUBLANG_DEFAULT),
                             reinterpret_cast<LPSTR>(&message), 0, nullptr);
  std::string out;
  if (len > 0 && message != nullptr) {
    out.assign(message, len);
    while (!out.empty() && (out.back() == '\n' || out.back() == '\r')) {
      out.pop_back();
    }
    LocalFree(message);
  } else {
    out = "Windows error " + std::to_string(err);
  }
  return out;
}
#endif

class SharedLibrary {
 public:
  explicit SharedLibrary(const char* name) : name_(name) {
#ifdef _WIN32
    handle_ = LoadLibraryA(name);
    if (!handle_) {
      throw std::runtime_error(std::string("cannot load ") + name + ": " + LastWindowsError());
    }
#else
    handle_ = dlopen(name, RTLD_NOW | RTLD_LOCAL);
    if (!handle_) {
      throw std::runtime_error(std::string("cannot load ") + name + ": " + dlerror());
    }
#endif
  }

  ~SharedLibrary() {
    if (handle_) {
#ifdef _WIN32
      FreeLibrary(handle_);
#else
      dlclose(handle_);
#endif
    }
  }

  SharedLibrary(const SharedLibrary&) = delete;
  SharedLibrary& operator=(const SharedLibrary&) = delete;

  template <typename T>
  T Symbol(const char* symbol) {
#ifdef _WIN32
    FARPROC ptr = GetProcAddress(handle_, symbol);
    if (ptr == nullptr) {
      throw std::runtime_error(name_ + " is missing symbol " + symbol + ": " + LastWindowsError());
    }
    return reinterpret_cast<T>(ptr);
#else
    dlerror();
    void* ptr = dlsym(handle_, symbol);
    const char* err = dlerror();
    if (err != nullptr || ptr == nullptr) {
      throw std::runtime_error(name_ + " is missing symbol " + symbol);
    }
    return reinterpret_cast<T>(ptr);
#endif
  }

 private:
  std::string name_;
#ifdef _WIN32
  HMODULE handle_ = nullptr;
#else
  void* handle_ = nullptr;
#endif
};

std::string NVENCStatusName(NVENCSTATUS status) {
  switch (status) {
    case NV_ENC_SUCCESS:
      return "NV_ENC_SUCCESS";
    case NV_ENC_ERR_NO_ENCODE_DEVICE:
      return "NV_ENC_ERR_NO_ENCODE_DEVICE";
    case NV_ENC_ERR_UNSUPPORTED_DEVICE:
      return "NV_ENC_ERR_UNSUPPORTED_DEVICE";
    case NV_ENC_ERR_INVALID_ENCODERDEVICE:
      return "NV_ENC_ERR_INVALID_ENCODERDEVICE";
    case NV_ENC_ERR_INVALID_DEVICE:
      return "NV_ENC_ERR_INVALID_DEVICE";
    case NV_ENC_ERR_DEVICE_NOT_EXIST:
      return "NV_ENC_ERR_DEVICE_NOT_EXIST";
    case NV_ENC_ERR_INVALID_PTR:
      return "NV_ENC_ERR_INVALID_PTR";
    case NV_ENC_ERR_INVALID_PARAM:
      return "NV_ENC_ERR_INVALID_PARAM";
    case NV_ENC_ERR_INVALID_CALL:
      return "NV_ENC_ERR_INVALID_CALL";
    case NV_ENC_ERR_OUT_OF_MEMORY:
      return "NV_ENC_ERR_OUT_OF_MEMORY";
    case NV_ENC_ERR_ENCODER_NOT_INITIALIZED:
      return "NV_ENC_ERR_ENCODER_NOT_INITIALIZED";
    case NV_ENC_ERR_UNSUPPORTED_PARAM:
      return "NV_ENC_ERR_UNSUPPORTED_PARAM";
    case NV_ENC_ERR_LOCK_BUSY:
      return "NV_ENC_ERR_LOCK_BUSY";
    case NV_ENC_ERR_NOT_ENOUGH_BUFFER:
      return "NV_ENC_ERR_NOT_ENOUGH_BUFFER";
    case NV_ENC_ERR_INVALID_VERSION:
      return "NV_ENC_ERR_INVALID_VERSION";
    case NV_ENC_ERR_NEED_MORE_INPUT:
      return "NV_ENC_ERR_NEED_MORE_INPUT";
    case NV_ENC_ERR_ENCODER_BUSY:
      return "NV_ENC_ERR_ENCODER_BUSY";
    case NV_ENC_ERR_GENERIC:
      return "NV_ENC_ERR_GENERIC";
    default:
      return "NVENCSTATUS(" + std::to_string(static_cast<int>(status)) + ")";
  }
}

void CheckNVENC(NVENCSTATUS status, const char* call) {
  if (status != NV_ENC_SUCCESS) {
    throw std::runtime_error(std::string(call) + " failed: " + NVENCStatusName(status));
  }
}

void CheckCUDA(CUresult status, const char* call) {
  if (status != CUDA_SUCCESS_VALUE) {
    throw std::runtime_error(std::string(call) + " failed with CUDA status " + std::to_string(status));
  }
}

std::pair<uint32_t, uint32_t> FPSRational(double fps) {
  if (fps <= 0) {
    return {30, 1};
  }
  const uint32_t den = 1000;
  uint32_t num = static_cast<uint32_t>(std::llround(fps * static_cast<double>(den)));
  if (num == 0) {
    num = 30000;
  }
  uint32_t a = num;
  uint32_t b = den;
  while (b != 0) {
    uint32_t t = a % b;
    a = b;
    b = t;
  }
  return {num / a, den / a};
}

class CUDAContext {
 public:
  CUDAContext() : cuda_(CUDALibraryName()) {
    cuInit_ = cuda_.Symbol<CuInitFn>("cuInit");
    cuDeviceGetCount_ = cuda_.Symbol<CuDeviceGetCountFn>("cuDeviceGetCount");
    cuDeviceGet_ = cuda_.Symbol<CuDeviceGetFn>("cuDeviceGet");
    cuCtxCreate_ = cuda_.Symbol<CuCtxCreateFn>("cuCtxCreate_v2");
    cuCtxDestroy_ = cuda_.Symbol<CuCtxDestroyFn>("cuCtxDestroy_v2");

    CheckCUDA(cuInit_(0), "cuInit");
    int count = 0;
    CheckCUDA(cuDeviceGetCount_(&count), "cuDeviceGetCount");
    if (count <= 0) {
      throw std::runtime_error("CUDA did not report any devices");
    }
    CUdevice device = 0;
    CheckCUDA(cuDeviceGet_(&device, 0), "cuDeviceGet");
    CheckCUDA(cuCtxCreate_(&context_, 0, device), "cuCtxCreate_v2");
  }

  ~CUDAContext() {
    if (context_) {
      (void)cuCtxDestroy_(context_);
    }
  }

  CUDAContext(const CUDAContext&) = delete;
  CUDAContext& operator=(const CUDAContext&) = delete;

  CUcontext context() const { return context_; }

 private:
  static const char* CUDALibraryName() {
#ifdef _WIN32
    return "nvcuda.dll";
#else
    return "libcuda.so.1";
#endif
  }

  SharedLibrary cuda_;
  CuInitFn cuInit_ = nullptr;
  CuDeviceGetCountFn cuDeviceGetCount_ = nullptr;
  CuDeviceGetFn cuDeviceGet_ = nullptr;
  CuCtxCreateFn cuCtxCreate_ = nullptr;
  CuCtxDestroyFn cuCtxDestroy_ = nullptr;
  CUcontext context_ = nullptr;
};

class NVENCSession {
 public:
  explicit NVENCSession(CUcontext cuda_context) : nvenc_(NVENCLibraryName()) {
    auto create_instance = nvenc_.Symbol<NvEncodeAPICreateInstanceFn>("NvEncodeAPICreateInstance");
    funcs_.version = NV_ENCODE_API_FUNCTION_LIST_VER;
    CheckNVENC(create_instance(&funcs_), "NvEncodeAPICreateInstance");

    NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS open = {};
    open.version = NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS_VER;
    open.deviceType = NV_ENC_DEVICE_TYPE_CUDA;
    open.device = cuda_context;
    open.apiVersion = NVENCAPI_VERSION;
    CheckNVENC(funcs_.nvEncOpenEncodeSessionEx(&open, &encoder_), "nvEncOpenEncodeSessionEx");
  }

  ~NVENCSession() {
    if (bitstream_) {
      (void)funcs_.nvEncDestroyBitstreamBuffer(encoder_, bitstream_);
    }
    if (input_) {
      (void)funcs_.nvEncDestroyInputBuffer(encoder_, input_);
    }
    if (encoder_) {
      (void)funcs_.nvEncDestroyEncoder(encoder_);
    }
  }

  NVENCSession(const NVENCSession&) = delete;
  NVENCSession& operator=(const NVENCSession&) = delete;

  void Initialize(const EncodeOptions& opts) {
    if (!funcs_.nvEncGetEncodeCaps || !funcs_.nvEncInitializeEncoder ||
        !funcs_.nvEncCreateInputBuffer || !funcs_.nvEncCreateBitstreamBuffer) {
      throw std::runtime_error("NVENC function list is incomplete");
    }

    NV_ENC_CAPS_PARAM caps = {};
    caps.version = NV_ENC_CAPS_PARAM_VER;
    caps.capsToQuery = NV_ENC_CAPS_SUPPORT_EMPHASIS_LEVEL_MAP;
    int emphasis_supported = 0;
    CheckNVENC(funcs_.nvEncGetEncodeCaps(encoder_, NV_ENC_CODEC_H264_GUID, &caps, &emphasis_supported),
               "nvEncGetEncodeCaps(NV_ENC_CAPS_SUPPORT_EMPHASIS_LEVEL_MAP)");
    if (emphasis_supported == 0) {
      throw std::runtime_error("GPU/NVENC driver does not support NV_ENC_CAPS_SUPPORT_EMPHASIS_LEVEL_MAP");
    }

    NV_ENC_PRESET_CONFIG preset = {};
    preset.version = NV_ENC_PRESET_CONFIG_VER;
    preset.presetCfg.version = NV_ENC_CONFIG_VER;
    if (!funcs_.nvEncGetEncodePresetConfigEx) {
      throw std::runtime_error("NVENC driver function list does not expose NvEncGetEncodePresetConfigEx");
    }
    CheckNVENC(funcs_.nvEncGetEncodePresetConfigEx(encoder_, NV_ENC_CODEC_H264_GUID,
                                                   NV_ENC_PRESET_P4_GUID,
                                                   NV_ENC_TUNING_INFO_HIGH_QUALITY, &preset),
               "nvEncGetEncodePresetConfigEx");

    NV_ENC_CONFIG config = preset.presetCfg;
    config.version = NV_ENC_CONFIG_VER;
    config.profileGUID = NV_ENC_H264_PROFILE_HIGH_GUID;
    config.gopLength = 60;
    config.frameIntervalP = 1;
    config.frameFieldMode = NV_ENC_PARAMS_FRAME_FIELD_MODE_FRAME;
    config.mvPrecision = NV_ENC_MV_PRECISION_QUARTER_PEL;
    config.rcParams.version = NV_ENC_RC_PARAMS_VER;
    config.rcParams.rateControlMode = NV_ENC_PARAMS_RC_VBR;
    config.rcParams.averageBitRate = static_cast<uint32_t>(std::max(1, opts.bitrate_kbps) * 1000);
    config.rcParams.maxBitRate = static_cast<uint32_t>(std::max(1, opts.bitrate_kbps) * 1150);
    config.rcParams.vbvBufferSize = static_cast<uint32_t>(std::max(1, opts.bitrate_kbps) * 2000);
    config.rcParams.enableAQ = 0;
    config.rcParams.enableTemporalAQ = 0;
    config.rcParams.qpMapMode = NV_ENC_QP_MAP_EMPHASIS;
    config.encodeCodecConfig.h264Config.level = NV_ENC_LEVEL_AUTOSELECT;
    config.encodeCodecConfig.h264Config.chromaFormatIDC = 1;
    config.encodeCodecConfig.h264Config.idrPeriod = 60;
    config.encodeCodecConfig.h264Config.repeatSPSPPS = 1;
    config.encodeCodecConfig.h264Config.outputAUD = 1;
    config.encodeCodecConfig.h264Config.entropyCodingMode = NV_ENC_H264_ENTROPY_CODING_MODE_CABAC;
    config.encodeCodecConfig.h264Config.bdirectMode = NV_ENC_H264_BDIRECT_MODE_DISABLE;
    config.encodeCodecConfig.h264Config.adaptiveTransformMode = NV_ENC_H264_ADAPTIVE_TRANSFORM_AUTOSELECT;
    config.encodeCodecConfig.h264Config.stereoMode = NV_ENC_STEREO_PACKING_MODE_NONE;

    auto fps = FPSRational(opts.fps);
    NV_ENC_INITIALIZE_PARAMS init = {};
    init.version = NV_ENC_INITIALIZE_PARAMS_VER;
    init.encodeGUID = NV_ENC_CODEC_H264_GUID;
    init.presetGUID = NV_ENC_PRESET_P4_GUID;
    init.encodeWidth = static_cast<uint32_t>(opts.width);
    init.encodeHeight = static_cast<uint32_t>(opts.height);
    init.darWidth = static_cast<uint32_t>(opts.width);
    init.darHeight = static_cast<uint32_t>(opts.height);
    init.frameRateNum = fps.first;
    init.frameRateDen = fps.second;
    init.enableEncodeAsync = 0;
    init.enablePTD = 1;
    init.tuningInfo = NV_ENC_TUNING_INFO_HIGH_QUALITY;
    init.encodeConfig = &config;
    init.maxEncodeWidth = static_cast<uint32_t>(opts.width);
    init.maxEncodeHeight = static_cast<uint32_t>(opts.height);
    NVENCSTATUS init_status = funcs_.nvEncInitializeEncoder(encoder_, &init);
    if (init_status != NV_ENC_SUCCESS) {
      std::string detail;
      if (funcs_.nvEncGetLastErrorString) {
        const char* last = funcs_.nvEncGetLastErrorString(encoder_);
        if (last && *last) {
          detail = std::string(": ") + last;
        }
      }
      throw std::runtime_error("nvEncInitializeEncoder failed: " + NVENCStatusName(init_status) + detail);
    }

    NV_ENC_CREATE_INPUT_BUFFER input = {};
    input.version = NV_ENC_CREATE_INPUT_BUFFER_VER;
    input.width = static_cast<uint32_t>(opts.width);
    input.height = static_cast<uint32_t>(opts.height);
    input.bufferFmt = NV_ENC_BUFFER_FORMAT_NV12;
    CheckNVENC(funcs_.nvEncCreateInputBuffer(encoder_, &input), "nvEncCreateInputBuffer");
    input_ = input.inputBuffer;

    NV_ENC_CREATE_BITSTREAM_BUFFER bitstream = {};
    bitstream.version = NV_ENC_CREATE_BITSTREAM_BUFFER_VER;
    CheckNVENC(funcs_.nvEncCreateBitstreamBuffer(encoder_, &bitstream), "nvEncCreateBitstreamBuffer");
    bitstream_ = bitstream.bitstreamBuffer;
  }

  void EncodeFrame(const std::vector<uint8_t>& frame, int width, int height, int frame_index,
                   std::vector<int8_t>& emphasis_map, std::ofstream& out) {
    NV_ENC_LOCK_INPUT_BUFFER lock_input = {};
    lock_input.version = NV_ENC_LOCK_INPUT_BUFFER_VER;
    lock_input.inputBuffer = input_;
    CheckNVENC(funcs_.nvEncLockInputBuffer(encoder_, &lock_input), "nvEncLockInputBuffer");

    auto* dst = static_cast<uint8_t*>(lock_input.bufferDataPtr);
    if (!dst || lock_input.pitch == 0) {
      throw std::runtime_error("nvEncLockInputBuffer returned invalid buffer");
    }
    const size_t y_size = static_cast<size_t>(width) * static_cast<size_t>(height);
    const uint8_t* src_y = frame.data();
    const uint8_t* src_uv = frame.data() + y_size;
    for (int y = 0; y < height; ++y) {
      std::memcpy(dst + static_cast<size_t>(y) * lock_input.pitch,
                  src_y + static_cast<size_t>(y) * width,
                  static_cast<size_t>(width));
    }
    uint8_t* dst_uv = dst + static_cast<size_t>(lock_input.pitch) * height;
    for (int y = 0; y < height / 2; ++y) {
      std::memcpy(dst_uv + static_cast<size_t>(y) * lock_input.pitch,
                  src_uv + static_cast<size_t>(y) * width,
                  static_cast<size_t>(width));
    }
    CheckNVENC(funcs_.nvEncUnlockInputBuffer(encoder_, input_), "nvEncUnlockInputBuffer");

    NV_ENC_PIC_PARAMS pic = {};
    pic.version = NV_ENC_PIC_PARAMS_VER;
    pic.inputWidth = static_cast<uint32_t>(width);
    pic.inputHeight = static_cast<uint32_t>(height);
    pic.inputPitch = lock_input.pitch;
    pic.frameIdx = static_cast<uint32_t>(frame_index);
    pic.inputTimeStamp = static_cast<uint64_t>(frame_index);
    pic.inputDuration = 1;
    pic.inputBuffer = input_;
    pic.outputBitstream = bitstream_;
    pic.bufferFmt = NV_ENC_BUFFER_FORMAT_NV12;
    pic.pictureStruct = NV_ENC_PIC_STRUCT_FRAME;
    if (frame_index == 0) {
      pic.encodePicFlags = NV_ENC_PIC_FLAG_FORCEIDR | NV_ENC_PIC_FLAG_OUTPUT_SPSPPS;
    }
    pic.qpDeltaMap = emphasis_map.data();
    pic.qpDeltaMapSize = static_cast<uint32_t>(emphasis_map.size());

    NVENCSTATUS enc_status = funcs_.nvEncEncodePicture(encoder_, &pic);
    if (enc_status == NV_ENC_ERR_NEED_MORE_INPUT) {
      return;
    }
    CheckNVENC(enc_status, "nvEncEncodePicture");
    DrainBitstream(out);
  }

  void Flush(std::ofstream& out) {
    (void)out;
    NV_ENC_PIC_PARAMS eos = {};
    eos.version = NV_ENC_PIC_PARAMS_VER;
    eos.encodePicFlags = NV_ENC_PIC_FLAG_EOS;
    NVENCSTATUS status = funcs_.nvEncEncodePicture(encoder_, &eos);
    if (status != NV_ENC_SUCCESS && status != NV_ENC_ERR_NEED_MORE_INPUT) {
      CheckNVENC(status, "nvEncEncodePicture(EOS)");
    }
  }

 private:
  static const char* NVENCLibraryName() {
#ifdef _WIN32
    return "nvEncodeAPI64.dll";
#else
    return "libnvidia-encode.so.1";
#endif
  }

  void DrainBitstream(std::ofstream& out) {
    NV_ENC_LOCK_BITSTREAM lock = {};
    lock.version = NV_ENC_LOCK_BITSTREAM_VER;
    lock.outputBitstream = bitstream_;
    CheckNVENC(funcs_.nvEncLockBitstream(encoder_, &lock), "nvEncLockBitstream");
    if (lock.bitstreamSizeInBytes > 0 && lock.bitstreamBufferPtr != nullptr) {
      out.write(static_cast<const char*>(lock.bitstreamBufferPtr),
                static_cast<std::streamsize>(lock.bitstreamSizeInBytes));
      if (!out) {
        throw std::runtime_error("failed to write encoded bitstream");
      }
    }
    CheckNVENC(funcs_.nvEncUnlockBitstream(encoder_, bitstream_), "nvEncUnlockBitstream");
  }

  SharedLibrary nvenc_;
  NV_ENCODE_API_FUNCTION_LIST funcs_ = {};
  void* encoder_ = nullptr;
  NV_ENC_INPUT_PTR input_ = nullptr;
  NV_ENC_OUTPUT_PTR bitstream_ = nullptr;
};

bool ReadExactFrame(std::istream& in, std::vector<uint8_t>& frame) {
  in.read(reinterpret_cast<char*>(frame.data()), static_cast<std::streamsize>(frame.size()));
  const std::streamsize got = in.gcount();
  if (got == 0 && in.eof()) {
    return false;
  }
  if (got != static_cast<std::streamsize>(frame.size())) {
    throw std::runtime_error("stdin ended with a partial NV12 frame");
  }
  return true;
}

void ValidateOptions(const EncodeOptions& opts) {
  if (opts.width <= 0 || opts.height <= 0) {
    throw std::runtime_error("video dimensions must be positive");
  }
  if ((opts.width % 2) != 0 || (opts.height % 2) != 0) {
    throw std::runtime_error("NV12/H.264 encode requires even frame dimensions");
  }
  if (opts.fps <= 0) {
    throw std::runtime_error("--fps must be positive");
  }
  if (opts.bitrate_kbps <= 0) {
    throw std::runtime_error("--bitrate-kbps must be positive");
  }
  if (opts.block_size <= 0) {
    throw std::runtime_error("--block-size must be positive");
  }
  if (opts.roi_blocks.empty()) {
    throw std::runtime_error("--roi-blocks must contain at least one block");
  }
  if (opts.output.empty()) {
    throw std::runtime_error("--output is required");
  }
}

}  // namespace

void EncodeStdinNV12ToH264(const EncodeOptions& opts) {
  ValidateOptions(opts);

  auto emphasis = BuildEmphasisMap(opts.width, opts.height, opts.block_size, opts.roi_blocks);
  std::vector<int8_t> emphasis_i8(emphasis.levels.begin(), emphasis.levels.end());

  CUDAContext cuda;
  NVENCSession session(cuda.context());
  session.Initialize(opts);

  std::ofstream out(opts.output, std::ios::binary);
  if (!out) {
    throw std::runtime_error("cannot open output file: " + opts.output);
  }

  const size_t frame_size = static_cast<size_t>(opts.width) * static_cast<size_t>(opts.height) * 3 / 2;
  std::vector<uint8_t> frame(frame_size);
  int frame_index = 0;
  while (ReadExactFrame(std::cin, frame)) {
    session.EncodeFrame(frame, opts.width, opts.height, frame_index, emphasis_i8, out);
    ++frame_index;
  }
  session.Flush(out);

  std::cerr << "roi-nvenc: encoded " << frame_index
            << " frames with NVIDIA Video Codec SDK Emphasis MAP\n";
}

}  // namespace roi_nvenc
