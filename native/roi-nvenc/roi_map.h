#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace roi_nvenc {

struct ROIBlock {
  int col = 0;
  int row = 0;
  int w = 1;
  int h = 1;
  double qoffset = 0.0;
};

struct EmphasisMap {
  int width = 0;
  int height = 0;
  std::vector<uint8_t> levels;
};

uint8_t EmphasisLevelForQOffset(double qoffset);
std::vector<ROIBlock> ParseROIBlocks(const std::string& value);
std::string SerializeROIBlocks(const std::vector<ROIBlock>& blocks);
EmphasisMap BuildEmphasisMap(int width, int height, int block_size, const std::vector<ROIBlock>& blocks);

}  // namespace roi_nvenc
