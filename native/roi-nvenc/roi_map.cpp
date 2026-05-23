#include "roi_map.h"

#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <sstream>
#include <stdexcept>

namespace roi_nvenc {

namespace {

int CeilDiv(int value, int divisor) {
  return (value + divisor - 1) / divisor;
}

int NormalizedSpan(int value) {
  return value <= 0 ? 1 : value;
}

int ParseInt(const std::string& value, const char* field) {
  char* end = nullptr;
  long parsed = std::strtol(value.c_str(), &end, 10);
  if (end == value.c_str() || *end != '\0') {
    throw std::runtime_error(std::string("invalid ROI block ") + field + ": " + value);
  }
  return static_cast<int>(parsed);
}

double ParseDouble(const std::string& value, const char* field) {
  char* end = nullptr;
  double parsed = std::strtod(value.c_str(), &end);
  if (end == value.c_str() || *end != '\0') {
    throw std::runtime_error(std::string("invalid ROI block ") + field + ": " + value);
  }
  return parsed;
}

}  // namespace

uint8_t EmphasisLevelForQOffset(double qoffset) {
  if (qoffset <= -0.35) {
    return 5;
  }
  if (qoffset <= -0.20) {
    return 3;
  }
  if (qoffset < 0.0) {
    return 1;
  }
  return 0;
}

std::vector<ROIBlock> ParseROIBlocks(const std::string& value) {
  if (value.empty()) {
    throw std::runtime_error("--roi-blocks must not be empty");
  }

  std::vector<ROIBlock> blocks;
  std::stringstream all(value);
  std::string item;
  while (std::getline(all, item, ';')) {
    if (item.empty()) {
      continue;
    }

    std::stringstream one(item);
    std::string part;
    std::vector<std::string> parts;
    while (std::getline(one, part, ',')) {
      parts.push_back(part);
    }
    if (parts.size() != 5) {
      throw std::runtime_error("each ROI block must be col,row,w,h,qoffset");
    }

    ROIBlock block;
    block.col = ParseInt(parts[0], "col");
    block.row = ParseInt(parts[1], "row");
    block.w = NormalizedSpan(ParseInt(parts[2], "w"));
    block.h = NormalizedSpan(ParseInt(parts[3], "h"));
    block.qoffset = ParseDouble(parts[4], "qoffset");
    blocks.push_back(block);
  }

  if (blocks.empty()) {
    throw std::runtime_error("--roi-blocks must contain at least one block");
  }
  return blocks;
}

std::string SerializeROIBlocks(const std::vector<ROIBlock>& blocks) {
  std::ostringstream out;
  for (size_t i = 0; i < blocks.size(); ++i) {
    if (i > 0) {
      out << ';';
    }
    char qoffset[64];
    std::snprintf(qoffset, sizeof(qoffset), "%.4f", blocks[i].qoffset);
    out << blocks[i].col << ',' << blocks[i].row << ',' << NormalizedSpan(blocks[i].w) << ','
        << NormalizedSpan(blocks[i].h) << ',' << qoffset;
  }
  return out.str();
}

EmphasisMap BuildEmphasisMap(int width, int height, int block_size, const std::vector<ROIBlock>& blocks) {
  if (width <= 0 || height <= 0) {
    throw std::runtime_error("video dimensions must be positive");
  }
  if (block_size <= 0) {
    throw std::runtime_error("--block-size must be positive");
  }

  const int macroblock = 16;
  const int mb_width = CeilDiv(width, macroblock);
  const int mb_height = CeilDiv(height, macroblock);
  const int grid_cols = CeilDiv(width, block_size);
  const int grid_rows = CeilDiv(height, block_size);

  EmphasisMap map;
  map.width = mb_width;
  map.height = mb_height;
  map.levels.assign(static_cast<size_t>(mb_width * mb_height), 0);

  for (size_t i = 0; i < blocks.size(); ++i) {
    const ROIBlock& block = blocks[i];
    const int w_blocks = NormalizedSpan(block.w);
    const int h_blocks = NormalizedSpan(block.h);
    if (block.col < 0 || block.row < 0) {
      throw std::runtime_error("ROI block col and row must be non-negative");
    }
    if (block.col >= grid_cols || block.row >= grid_rows ||
        block.col + w_blocks > grid_cols || block.row + h_blocks > grid_rows) {
      throw std::runtime_error("ROI block extends outside the frame block grid");
    }

    const uint8_t level = EmphasisLevelForQOffset(block.qoffset);
    if (level == 0) {
      continue;
    }

    const int x0 = block.col * block_size;
    const int y0 = block.row * block_size;
    const int x1 = std::min(width, x0 + w_blocks * block_size);
    const int y1 = std::min(height, y0 + h_blocks * block_size);
    const int mb_x0 = x0 / macroblock;
    const int mb_y0 = y0 / macroblock;
    const int mb_x1 = CeilDiv(x1, macroblock);
    const int mb_y1 = CeilDiv(y1, macroblock);

    for (int y = mb_y0; y < mb_y1; ++y) {
      for (int x = mb_x0; x < mb_x1; ++x) {
        const size_t idx = static_cast<size_t>(y * mb_width + x);
        map.levels[idx] = std::max(map.levels[idx], level);
      }
    }
  }

  return map;
}

}  // namespace roi_nvenc
