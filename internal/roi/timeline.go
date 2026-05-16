package roi

func normalizedROITimeline(selection ROISelection, info VideoInfo) []TimedROI {
	if len(selection.Timeline) <= 1 {
		return nil
	}

	out := make([]TimedROI, 0, len(selection.Timeline))
	for _, item := range selection.Timeline {
		if item.EndSeconds <= item.StartSeconds {
			continue
		}

		item.ROI = clampROI(item.ROI, info)
		out = append(out, item)
	}

	if len(out) <= 1 {
		return nil
	}
	return out
}

func roiAtTime(selection ROISelection, t float64, info VideoInfo) ROI {
	for _, item := range normalizedROITimeline(selection, info) {
		if t >= item.StartSeconds && t < item.EndSeconds {
			return item.ROI
		}
	}

	return selection.ROI
}
