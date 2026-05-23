#pragma once

#include "roi_map.h"

#include <string>
#include <vector>

namespace roi_nvenc {

struct EncodeOptions {
  int width = 0;
  int height = 0;
  double fps = 0.0;
  int bitrate_kbps = 0;
  int block_size = 0;
  std::vector<ROIBlock> roi_blocks;
  std::string output;
};

void EncodeStdinNV12ToH264(const EncodeOptions& opts);

}  // namespace roi_nvenc
