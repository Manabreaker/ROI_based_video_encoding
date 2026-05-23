#include "roi_map.h"
#include "nvenc_encoder.h"

#include <exception>
#include <iostream>
#include <string>
#include <vector>

namespace {

struct Options {
  bool self_test_roi_map = false;
  int width = 0;
  int height = 0;
  int block_size = 0;
  double fps = 0.0;
  int bitrate_kbps = 0;
  std::string roi_blocks;
  std::string input_format;
  std::string codec;
  std::string output;
};

void Usage(std::ostream& out) {
  out << "Usage:\n"
      << "  roi-nvenc --self-test-roi-map\n"
      << "  roi-nvenc --width W --height H --fps FPS --bitrate-kbps KBPS \\\n"
      << "            --block-size PX --roi-blocks col,row,w,h,qoffset[;...] \\\n"
      << "            --input-format nv12 --codec h264 --output out.h264\n";
}

int ParseIntArg(const std::string& name, const std::string& value) {
  try {
    return std::stoi(value);
  } catch (const std::exception&) {
    throw std::runtime_error(name + " must be an integer");
  }
}

double ParseDoubleArg(const std::string& name, const std::string& value) {
  try {
    return std::stod(value);
  } catch (const std::exception&) {
    throw std::runtime_error(name + " must be a number");
  }
}

Options ParseArgs(int argc, char** argv) {
  Options opts;
  for (int i = 1; i < argc; ++i) {
    std::string arg = argv[i];
    if (arg == "--self-test-roi-map") {
      opts.self_test_roi_map = true;
      continue;
    }
    if (arg == "--help" || arg == "-h") {
      Usage(std::cout);
      std::exit(0);
    }
    if (i + 1 >= argc) {
      throw std::runtime_error(arg + " requires a value");
    }
    std::string value = argv[++i];
    if (arg == "--width") {
      opts.width = ParseIntArg(arg, value);
    } else if (arg == "--height") {
      opts.height = ParseIntArg(arg, value);
    } else if (arg == "--fps") {
      opts.fps = ParseDoubleArg(arg, value);
    } else if (arg == "--bitrate-kbps") {
      opts.bitrate_kbps = ParseIntArg(arg, value);
    } else if (arg == "--block-size") {
      opts.block_size = ParseIntArg(arg, value);
    } else if (arg == "--roi-blocks") {
      opts.roi_blocks = value;
    } else if (arg == "--input-format") {
      opts.input_format = value;
    } else if (arg == "--codec") {
      opts.codec = value;
    } else if (arg == "--output") {
      opts.output = value;
    } else {
      throw std::runtime_error("unknown argument: " + arg);
    }
  }
  return opts;
}

void Expect(bool value, const std::string& message) {
  if (!value) {
    throw std::runtime_error("self-test failed: " + message);
  }
}

int RunROIMapSelfTest() {
  using roi_nvenc::BuildEmphasisMap;
  using roi_nvenc::EmphasisLevelForQOffset;
  using roi_nvenc::ParseROIBlocks;
  using roi_nvenc::ROIBlock;
  using roi_nvenc::SerializeROIBlocks;

  Expect(EmphasisLevelForQOffset(-0.40) == 5, "high emphasis qoffset");
  Expect(EmphasisLevelForQOffset(-0.20) == 3, "medium emphasis qoffset");
  Expect(EmphasisLevelForQOffset(-0.10) == 1, "low emphasis qoffset");
  Expect(EmphasisLevelForQOffset(0.10) == 0, "positive qoffset has no emphasis");

  std::vector<ROIBlock> blocks = ParseROIBlocks("0,0,1,1,-0.3500;1,0,1,1,-0.2000;0,0,1,1,0.1000");
  Expect(SerializeROIBlocks(blocks) == "0,0,1,1,-0.3500;1,0,1,1,-0.2000;0,0,1,1,0.1000",
         "parse/serialize round trip");

  auto map = BuildEmphasisMap(64, 32, 32, blocks);
  Expect(map.width == 4 && map.height == 2, "macroblock dimensions");
  Expect(map.levels[0] == 5 && map.levels[1] == 5, "first ROI block high emphasis");
  Expect(map.levels[2] == 3 && map.levels[3] == 3, "second ROI block medium emphasis");
  Expect(map.levels[4] == 5 && map.levels[5] == 5, "first block covers intersecting macroblock row");
  Expect(map.levels[6] == 3 && map.levels[7] == 3, "second block covers intersecting macroblock row");

  auto partial = BuildEmphasisMap(50, 18, 32, std::vector<ROIBlock>{{1, 0, 1, 1, -0.35}});
  Expect(partial.width == 4 && partial.height == 2, "partial frame macroblock dimensions");
  Expect(partial.levels[2] == 5 && partial.levels[3] == 5, "partial right edge emphasis");
  Expect(partial.levels[6] == 5 && partial.levels[7] == 5, "partial bottom row emphasis");

  bool rejected = false;
  try {
    (void)BuildEmphasisMap(64, 32, 32, std::vector<ROIBlock>{{2, 0, 1, 1, -0.35}});
  } catch (const std::exception&) {
    rejected = true;
  }
  Expect(rejected, "out-of-frame block rejected");

  std::cout << "roi-map self-test passed\n";
  return 0;
}

int RunEncode(const Options& opts) {
  if (opts.width <= 0 || opts.height <= 0 || opts.block_size <= 0 || opts.fps <= 0 ||
      opts.bitrate_kbps <= 0 || opts.roi_blocks.empty() || opts.input_format != "nv12" ||
      opts.codec != "h264" || opts.output.empty()) {
    Usage(std::cerr);
    throw std::runtime_error("missing or unsupported encode arguments");
  }

  roi_nvenc::EncodeOptions encode;
  encode.width = opts.width;
  encode.height = opts.height;
  encode.fps = opts.fps;
  encode.bitrate_kbps = opts.bitrate_kbps;
  encode.block_size = opts.block_size;
  encode.roi_blocks = roi_nvenc::ParseROIBlocks(opts.roi_blocks);
  encode.output = opts.output;
  roi_nvenc::EncodeStdinNV12ToH264(encode);
  return 0;
}

}  // namespace

int main(int argc, char** argv) {
  try {
    Options opts = ParseArgs(argc, argv);
    if (opts.self_test_roi_map) {
      return RunROIMapSelfTest();
    }
    return RunEncode(opts);
  } catch (const std::exception& ex) {
    std::cerr << "roi-nvenc: " << ex.what() << "\n";
    return 1;
  }
}
