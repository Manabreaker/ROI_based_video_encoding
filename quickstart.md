## 1. Проверка окружения

```bash
go version
ffmpeg -hide_banner -encoders | grep -E 'libx264|h264_nvenc'
ffprobe -version
git lfs pull
```

Минимально нужен FFmpeg с `libx264`. Для настоящего NVIDIA ROI через SDK дополнительно нужны NVIDIA driver, `g++`, `make`, `libcuda.so.1` и `libnvidia-encode.so.1`.

## 2. Сборка

```bash
go build -o roi-poc ./cmd/roi
go build -o roi-map-ui ./cmd/roi-map-ui
make roi-nvenc
```

Ожидаемый результат:

- `./roi-poc` - основной CLI;
- `./roi-map-ui` - локальный browser UI для разметки QP-map блоков;
- `native/roi-nvenc/roi-nvenc` - native helper для NVIDIA Video Codec SDK.

## 3. Самый быстрый CPU QP-map запуск

```bash
./roi-poc config/qp_block_map_example.yaml \
  --out out/quickstart_cpu_qpmap \
  --target-bitrate 500k \
  --roi-two-pass=false \
  --metrics=false --encoder h264_nvenc_sdk  --fit-roi=false --debug
```

Ожидаемый лог:

- `encoder: libx264`;
- `[4/4] Rendering ROI using encoder QP-map side data...`;
- `ROI QP-map candidate libx264/abr`;
- в конце только один артефакт.

Ожидаемый файл:

```text
out/quickstart_cpu_qpmap/roi_high_quality_region.mp4
```

Важно: `encoder: auto` в QP-map режиме автоматически выбирает `libx264`, даже если на машине есть FFmpeg `h264_nvenc`. Это сделано специально, потому что FFmpeg NVENC не потребляет ROI side data.

## 4. Debug-режим с отчетами

```bash
./roi-poc config/qp_block_map_example.yaml \
  --out out/quickstart_cpu_debug \
  --target-bitrate 500k \
  --roi-two-pass=false \
  --metrics=false \
  --debug=true
```

Ожидаемые файлы:

```text
out/quickstart_cpu_debug/roi_high_quality_region.mp4
out/quickstart_cpu_debug/comparison_baseline_vs_roi.mp4
out/quickstart_cpu_debug/roi_preview.png
out/quickstart_cpu_debug/bitrate_windows.json
out/quickstart_cpu_debug/report.json
```

Если добавить `--metrics=true`, появится еще:

```text
out/quickstart_cpu_debug/quality_roi_psnr.json
```

## 5. Реальный NVIDIA NVENC ROI через SDK

Этот режим использует не FFmpeg `h264_nvenc`, а отдельный backend `h264_nvenc_sdk`, который передает ROI в NVIDIA Video Codec SDK как Emphasis MAP.

```bash
make roi-nvenc

./roi-poc \
  --input examples/ball.mp4 \
  --out out/quickstart_nvenc_sdk \
  --mode blocks \
  --roi-control qp-map \
  --roi-block-size 64 \
  --roi-blocks '4,2,2,2,-0.40;3,1,1,3,-0.10;6,1,1,3,-0.10' \
  --target-bitrate 500k \
  --encoder h264_nvenc_sdk \
  --fit-roi=false \
  --roi-rate-control abr \
  --roi-two-pass=false \
  --metrics=false
```

Ожидаемый лог:

- `encoder: h264_nvenc_sdk`;
- `Rendering ROI with NVIDIA Video Codec SDK Emphasis MAP`;
- `SDK emphasis-map blocks`.

Ожидаемый файл:

```text
out/quickstart_nvenc_sdk/roi_high_quality_region.mp4
```

Для debug-отчета добавьте `--debug=true`; в `report.json` будет строка `NVIDIA Video Codec SDK Emphasis MAP`.

## 6. Обычный FFmpeg NVENC

FFmpeg `h264_nvenc` работает для mask/preprocessing режима:

```bash
./roi-poc \
  --input examples/ball.mp4 \
  --out out/quickstart_ffmpeg_nvenc_mask \
  --mode static \
  --roi 0.35,0.25,0.30,0.40 \
  --target-bitrate 500k \
  --encoder h264_nvenc \
  --roi-control mask \
  --fit-roi=false \
  --metrics=false
```

Ожидаемый файл:

```text
out/quickstart_ffmpeg_nvenc_mask/roi_high_quality_region.mp4
```

А вот `h264_nvenc + --roi-control qp-map` намеренно завершается ошибкой:

```bash
./roi-poc \
  --input examples/ball.mp4 \
  --out out/should_fail \
  --mode static \
  --roi 0.35,0.25,0.30,0.40 \
  --target-bitrate 500k \
  --encoder h264_nvenc \
  --roi-control qp-map
```

Ожидаемая ошибка:

```text
--roi-control qp-map uses FFmpeg ROI side data, but encoder h264_nvenc does not consume it
```

Это нормальное поведение: для NVIDIA QP-map ROI нужно использовать `--encoder h264_nvenc_sdk`.


## 7. UI для разметки ROI-блоков

```bash
./roi-map-ui \
  --input examples/ball.mp4 \
  --out out/quickstart_ui \
  --config-out config/roi_blocks_generated.yaml \
  --target-bitrate 500k \
  --encoder libx264
```

Откройте URL из консоли, разметьте несколько блоков и нажмите `Запустить`.

Ожидаемый результат:

- UI сохранит YAML-конфиг;
- в output-директории появится копия `roi_blocks_config.yaml`;
- появится итоговый файл `roi_high_quality_region.mp4`;
- результат откроется прямо на странице UI.

Сгенерированный UI YAML по умолчанию содержит:

```yaml
fit-roi: false
debug: false
metrics: false
serve: true
```

`serve: true` не блокирует UI-запуск: UI сам отдает итоговый файл через свой HTTP handler.

## 8. Полная демонстрация

```bash
make demo
```

Эта команда тяжелее, потому что рендерит 4K/1080p comparison-видео. Ожидаемые директории:

```text
out/demo/01_good_roi_qp_blocks
out/demo/02_bad_roi_mask
out/demo/03_dynamic_blur
out/demo/04_dynamic_qp_map
out/demo/05_face_tracking
```

В каждой директории должны быть:

```text
roi_high_quality_region.mp4
comparison_baseline_vs_roi.mp4
roi_preview.png
bitrate_windows.json
report.json
```

`make demo` был проверен полностью: все 5 директорий создаются, включая face tracking пример.

## 9. Матрица энкодеров

| Энкодер             | QP-map ROI |                    Mask/preprocessing | Комментарий                                                                            |
|---------------------|-----------:|--------------------------------------:|----------------------------------------------------------------------------------------|
| `libx264`           |         Да |                                    Да | Основной переносимый вариант; работает через FFmpeg `addroi`.                          |
| `h264_nvenc_sdk`    |         Да |                                   Нет | Настоящий NVIDIA Video Codec SDK Emphasis MAP; нужен `mode: blocks`, `fit-roi: false`. |
| `h264_nvenc`        |        Нет |                                    Да | FFmpeg NVENC не потребляет ROI side data; QP-map режим намеренно запрещен.             |
| `h264_amf`          |        Нет |          Да, если FFmpeg собран с AMF | Проверяется на AMD/Windows машине.                                                     |
| `h264_videotoolbox` |        Нет | Да, если FFmpeg собран с VideoToolbox | MacOS не поддерживает ROI, видео кодируется с равномерным качеством.                   |

## 10. Проверочные команды разработчика

Перед сдачей были выполнены:

```bash
go test ./... -count=1
make test-native
ROI_NVENC_INTEGRATION=1 go test ./internal/roi -run TestNVENCSDKIntegrationEncode -count=1
make demo
```

Также вручную проверены:

- default `--debug=false` создает только `roi_high_quality_region.mp4`;
- `--debug=true` создает comparison/preview/JSON reports;
- `--metrics=true` в debug-режиме создает `quality_roi_psnr.json`;
- UI `/api/config` и `/api/run` создают YAML и итоговое видео;
- `h264_nvenc + qp-map` возвращает понятную ошибку вместо молчаливого неправильного результата.

## 11. Частые проблемы

Если примерные видео не скачались:

```bash
git lfs pull
```

Если FFmpeg пишет `No such filter: drawtext`, установите полную сборку FFmpeg с `drawtext`.

Если `h264_nvenc_sdk` пишет, что не найден helper, выполните:

```bash
make roi-nvenc
```

Если `h264_nvenc_sdk` недоступен из-за драйвера, проверьте NVIDIA driver и наличие `libcuda.so.1` / `libnvidia-encode.so.1`.
