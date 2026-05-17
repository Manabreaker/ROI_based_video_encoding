.PHONY: demo demo-check demo-assets demo-build clean-demo

DEMO_DIR := out/demo
DEMO_BIN := $(DEMO_DIR)/_bin/roi-poc

DEMO_DYNAMIC_VIDEO := examples/dynamic/854671-hd_1920_1080_25fps.mp4
DEMO_ROI := 0.32,0.18,0.36,0.54
DEMO_TARGET_BITRATE := 2500k

DEMO_ASSETS := \
	examples/ball.mp4 \
	examples/dynamic/8707797-uhd_3840_2160_30fps.mp4 \
	examples/dynamic/4934336_Person_People_3840x2160.mp4 \
	$(DEMO_DYNAMIC_VIDEO)

DEMO_COMMON_ARGS := \
	--encoder libx264 \
	--preset veryfast \
	--roi-two-pass=false \
	--bitrate-window 2 \
	--overlay-bitrate=true \
	--metrics=false \
	--serve=false

DEMO_DYNAMIC_COMMON_ARGS := \
	--input $(DEMO_DYNAMIC_VIDEO) \
	--mode static \
	--roi $(DEMO_ROI) \
	--target-bitrate $(DEMO_TARGET_BITRATE) \
	--tolerance 0.07 \
	--roi-rate-control abr \
	--middle-margin 0.35 \
	$(DEMO_COMMON_ARGS)

demo: demo-check demo-assets demo-build
	@rm -rf "$(DEMO_DIR)/01_good_roi_qp_blocks" \
		"$(DEMO_DIR)/02_bad_roi_mask" \
		"$(DEMO_DIR)/03_dynamic_blur" \
		"$(DEMO_DIR)/04_dynamic_qp_map" \
		"$(DEMO_DIR)/05_face_tracking"
	@mkdir -p "$(DEMO_DIR)"
	@printf '%s\n' \
		'ROI demo outputs' \
		'' \
		'01_good_roi_qp_blocks: successful ROI example from config/my_own_config.yaml' \
		'02_bad_roi_mask: intentionally weak ROI/mask example from config/mask_example.yaml' \
		'03_dynamic_blur: 854671 video, ROI preserved with blurred periphery' \
		'04_dynamic_qp_map: same 854671 parameters, encoder-level QP-map ROI' \
		'05_face_tracking: face tracking from config/cv_face_example.yaml' \
		'' \
		'Open comparison_baseline_vs_roi.mp4 inside each directory.' \
		> "$(DEMO_DIR)/README.txt"
	@echo "[demo 1/5] Successful ROI example from config/my_own_config.yaml"
	"$(DEMO_BIN)" config/my_own_config.yaml --out "$(DEMO_DIR)/01_good_roi_qp_blocks" $(DEMO_COMMON_ARGS)
	@echo "[demo 2/5] Weak ROI/mask example from config/mask_example.yaml"
	"$(DEMO_BIN)" config/mask_example.yaml --out "$(DEMO_DIR)/02_bad_roi_mask" $(DEMO_COMMON_ARGS)
	@echo "[demo 3/5] Dynamic video quality reduction with blur"
	"$(DEMO_BIN)" $(DEMO_DYNAMIC_COMMON_ARGS) \
		--out "$(DEMO_DIR)/03_dynamic_blur" \
		--roi-control mask \
		--fit-roi=false \
		--periphery-scale 1 \
		--blur 8 \
		--middle-scale 1 \
		--middle-blur 3
	@echo "[demo 4/5] Dynamic video quality reduction with QP-map"
	"$(DEMO_BIN)" $(DEMO_DYNAMIC_COMMON_ARGS) \
		--out "$(DEMO_DIR)/04_dynamic_qp_map" \
		--roi-control qp-map \
		--roi-qoffset -0.30 \
		--roi-middle-qoffset -0.10
	@echo "[demo 5/5] Face tracking example from config/cv_face_example.yaml"
	"$(DEMO_BIN)" config/cv_face_example.yaml --out "$(DEMO_DIR)/05_face_tracking" $(DEMO_COMMON_ARGS)
	@echo
	@echo "Demo files are in $(DEMO_DIR)/"

demo-check:
	@command -v go >/dev/null 2>&1 || { echo "go is required"; exit 1; }
	@command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg is required"; exit 1; }
	@command -v ffprobe >/dev/null 2>&1 || { echo "ffprobe is required"; exit 1; }
	@ffmpeg -hide_banner -encoders 2>/dev/null | grep -qw libx264 || { echo "ffmpeg with libx264 encoder is required"; exit 1; }

demo-assets:
	@if [ -d .git ] && git lfs version >/dev/null 2>&1; then \
		echo "[demo] Pulling Git LFS assets if needed"; \
		git lfs pull; \
	fi
	@for file in $(DEMO_ASSETS); do \
		if [ ! -s "$$file" ]; then \
			echo "Missing demo asset: $$file"; \
			echo "Install Git LFS and run: git lfs pull"; \
			exit 1; \
		fi; \
		if head -n 1 "$$file" | grep -q '^version https://git-lfs.github.com/spec'; then \
			echo "Demo asset is still a Git LFS pointer: $$file"; \
			echo "Install Git LFS and run: git lfs pull"; \
			exit 1; \
		fi; \
	done

demo-build:
	@mkdir -p "$(dir $(DEMO_BIN))"
	go build -o "$(DEMO_BIN)" ./cmd/roi

clean-demo:
	rm -rf "$(DEMO_DIR)"
