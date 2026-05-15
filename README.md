# ROI_based_video_encoding

CLI на Go для демонстрации ROI-based video encoding. Программа берет входное видео как baseline, создает ROI-вариант с повышенным качеством выбранной области и сохраняет side-by-side comparison, preview и JSON-отчеты.

Важно: это PoC на базе FFmpeg preprocessing, а не encoder-level ROI через QP-map. ROI сохраняется лучше за счет маски качества: выбранная область берется из исходного кадра, вокруг нее добавляется средняя зона, а периферия упрощается перед финальным H.264-кодированием.

Проект не требует реализации потоковой передачи видео. Основной результат - воспроизводимое кодирование видеофайла или другого FFmpeg-readable input в локальные output-артефакты.

Общее описание проекта, архитектуры и ограничений находится в [docs/overview.md](docs/overview.md).

## Требования

- Go 1.26 или совместимая версия, доступная в `PATH`;
- FFmpeg и FFprobe;
- Git LFS;
- Docker, если нужен запуск без локальной установки Go/FFmpeg.

Проверка окружения:

```bash
go version
ffmpeg -version
ffprobe -version
git lfs --version
```

Примеры видео представлены LFS pointer-файлами, скачайте реальные файлы:

```bash
git lfs pull
```

## Быстрый запуск

Соберите CLI:

```bash
go build -o roi-poc ./cmd/roi
```

Запустите обработку на примере из репозитория:

```bash
./roi-poc \
  --input examples/ball.mp4 \
  --out out/ball_roi \
  --mode static \
  --roi 0.35,0.25,0.30,0.40 \
  --target-bitrate 500k \
  --bitrate-window 2 \
  --metrics=false \
  --encoder libx264
```

Главный файл для просмотра результата:

```text
out/ball_roi/comparison_baseline_vs_roi.mp4
```

## Запуск через YAML config

Любой CLI-флаг можно перенести в YAML-файл: ключи называются так же, как флаги, но без `--`.

```yaml
input: examples/ball.mp4
out: out/ball_roi
mode: static
roi: 0.35,0.25,0.30,0.40
target-bitrate: 500k
bitrate-window: 2
metrics: false
encoder: libx264
fit-iterations: 5
```

Запуск:

```bash
./roi-poc --config roi.yaml
```

Также можно передать YAML как позиционный аргумент:

```bash
./roi-poc roi.yaml
```

Если значение указано и в YAML, и во флагах, побеждает флаг:

```bash
./roi-poc --config roi.yaml --target-bitrate 800k --metrics=true
```

## Что появится в output-директории

После успешного запуска в `--out` создаются:

```text
roi_high_quality_region.mp4
comparison_baseline_vs_roi.mp4
roi_preview.png
bitrate_windows.json
report.json
quality_roi_psnr.json      # только если --metrics=true и метрики посчитались
```

`comparison_baseline_vs_roi.mp4` показывает:

- слева: исходное видео как baseline, без перекодирования;
- справа: ROI output;
- текущий bitrate по временным окнам;
- средний/source bitrate, target/actual bitrate и размеры файлов;
- разметку зон качества: зеленая ROI, оранжевая middle ring, красная periphery.

## Основные сценарии запуска

Статическая ROI в пикселях:

```bash
./roi-poc \
  --input input.mp4 \
  --out out/static_pixels \
  --mode static \
  --roi 640,300,520,360
```

Статическая ROI в долях кадра:

```bash
./roi-poc \
  --input input.mp4 \
  --out out/static_fraction \
  --mode static \
  --roi 0.30,0.20,0.40,0.45
```

Если `--mode static`, но `--roi` не задан, используется центральная ROI.

Motion ROI:

```bash
./roi-poc \
  --input input.mp4 \
  --out out/motion \
  --mode motion \
  --motion-window 0.7 \
  --motion-threshold 34 \
  --roi-margin 0.18
```

Motion-режим извлекает два кадра, считает разницу яркости, строит bounding box изменившихся пикселей и расширяет его через `--roi-margin`. Это простая эвристика, а не CV/ML-детектор.

Запуск с локальным HTTP preview:

```bash
./roi-poc \
  --input examples/ball.mp4 \
  --out out/demo \
  --mode static \
  --serve
```

После обработки откройте:

```text
http://localhost:8080/comparison_baseline_vs_roi.mp4
```

Порт меняется через `--http`, например `--http :9090`.

## Битрейт и качество

Основной режим по умолчанию:

```bash
--roi-rate-control abr
--target-bitrate 1000k
```

В ABR-режиме ROI output кодируется около заданного битрейта. Для `libx264` используется two-pass, если включен `--roi-two-pass=true`; аппаратные энкодеры (`h264_nvenc`, `h264_amf`, `h264_videotoolbox`) используют single-pass ABR.

Fixed-quality режим:

```bash
--roi-rate-control crf
--roi-crf 16
```

По умолчанию включен fitting:

```bash
--fit-roi=true
```

В этом режиме программа не проходит всю лестницу качества линейно. Она использует interpolation search: сначала измеряет несколько опорных вариантов, затем по измеренному bitrate прыгает к кандидату, который должен быть ближе к `--target-bitrate`, и при необходимости проверяет соседние уровни. Лимит probe-ов задается через `--fit-iterations`.

Если нужно задать параметры вручную:

```bash
--fit-roi=false
--periphery-scale 0.35
--blur 2
```

При `--metrics=true` программа считает ROI-crop PSNR через FFmpeg `psnr` filter и пишет `quality_roi_psnr.json`.

## Важные флаги

| Флаг                   | По умолчанию | Назначение                                                     |
|------------------------|--------------|----------------------------------------------------------------|
| `--input`              | -            | входной видеофайл, URL, RTSP или другой FFmpeg-readable source |
| `--config`             | -            | YAML config; явно переданные флаги имеют приоритет             |
| `--out`                | `out`        | директория для результата                                      |
| `--mode`               | `static`     | режим ROI: `static`, `motion` или `blocks`                     |
| `--roi`                | -            | ROI как `x,y,w,h`, в пикселях или долях кадра                  |
| `--roi-block-size`     | `64`         | размер блока для `--mode blocks`                               |
| `--roi-blocks`         | -            | QP-map блоки: `col,row,qoffset` или `col,row,w,h,qoffset`      |
| `--target-bitrate`     | `1000k`      | целевой bitrate для ROI output                                 |
| `--roi-control`        | `qp-map`     | `qp-map` или старый `mask` preprocessing                       |
| `--roi-qoffset`        | `-0.30`      | QP offset для основной ROI в `qp-map` режиме                   |
| `--roi-middle-qoffset` | `-0.10`      | QP offset для middle ring в `qp-map` режиме                    |
| `--fit-roi`            | `true`       | подбирать параметры периферии около target bitrate             |
| `--roi-rate-control`   | `abr`        | `abr` или `crf`                                                |
| `--roi-crf`            | `16`         | CRF для ROI output в fixed-quality режиме                      |
| `--middle-margin`      | `0.35`       | расширение оранжевой middle-zone вокруг ROI                    |
| `--middle-scale`       | `0.67`       | scale для middle-zone перед обратным upscale                   |
| `--middle-blur`        | `1`          | blur для middle-zone                                           |
| `--periphery-scale`    | `0.35`       | scale периферии при `--fit-roi=false`                          |
| `--blur`               | `2`          | blur периферии при `--fit-roi=false`                           |
| `--encoder`            | `auto`       | `auto`, `libx264`, `h264_nvenc`, `h264_amf` или `h264_videotoolbox` |
| `--overlay-bitrate`    | `true`       | рисовать текущий bitrate на comparison-видео                   |
| `--bitrate-window`     | `1.0`        | размер окна bitrate в секундах                                 |
| `--metrics`            | `true`       | считать ROI PSNR report                                        |
| `--serve`              | `false`      | поднять локальный HTTP file server после обработки             |
| `--fit-iterations`     | `9`          | максимум probe-ов interpolation search для ROI fitting         |

Полный список флагов можно посмотреть через:

```bash
./roi-poc -h
```

## Энкодеры

Флаг:

```bash
--encoder auto
```

Поведение:

- `auto` проверяет `ffmpeg -hide_banner -encoders`;
- на macOS при наличии `h264_videotoolbox` используется VideoToolbox;
- иначе при наличии `h264_nvenc` используется NVIDIA NVENC;
- иначе при наличии `h264_amf` используется AMD AMF;
- если аппаратный H.264 энкодер недоступен, используется `libx264`;
- `--encoder libx264` принудительно включает CPU encoding;
- `--encoder h264_nvenc`, `--encoder h264_amf` и `--encoder h264_videotoolbox` завершатся ошибкой, если FFmpeg не поддерживает выбранный энкодер.

NVENC preset задается так:

```bash
--nvenc-preset p4
```

Примеры явного выбора аппаратного backend:

```bash
--encoder h264_amf
--encoder h264_videotoolbox
```

## Docker

Docker полезен для воспроизводимого окружения: образ собирает Go CLI и содержит FFmpeg/FFprobe.

Сборка:

```bash
docker build -t roi-poc .
```

Запуск:

```bash
docker run --rm -v "$PWD:/work" roi-poc \
  --input examples/ball.mp4 \
  --out out/docker_ball \
  --mode static \
  --target-bitrate 500k
```

GPU-кодирование изнутри Docker требует NVIDIA runtime на хосте:

```bash
docker run --rm --gpus all -v "$PWD:/work" roi-poc \
  --input examples/ball.mp4 \
  --out out/docker_nvenc \
  --encoder h264_nvenc \
  --target-bitrate 500k
```

## Проверки для разработки

```bash
go test ./...
go test ./... -cover
go vet ./...
```

## Troubleshooting

### FFmpeg на macOS без drawtext

Если FFmpeg установлен в облегченной сборке, comparison render может упасть с ошибкой:

```text
No such filter: 'drawtext'
```

Переустановите полную сборку:

```bash
brew uninstall ffmpeg
brew install homebrew-ffmpeg/ffmpeg/ffmpeg
```

### Видео из examples не открываются

Проверьте, что Git LFS установлен и реальные файлы скачаны:

```bash
git lfs install
git lfs pull
```

### Аппаратный энкодер не выбирается

Проверьте, что FFmpeg видит энкодер:

```bash
ffmpeg -hide_banner -encoders | grep -E 'h264_(nvenc|amf|videotoolbox)'
```

Если нужного энкодера нет, используйте CPU:

```bash
--encoder libx264
```

## Структура документации

В этом репозитории используется практичная схема:

- [README.md](README.md) - как собрать, запустить и проверить результат;
- [docs/overview.md](docs/overview.md) - обзор проекта, архитектуры, pipeline и ограничений;
- [docs/research.md](docs/research.md) - исследовательский контекст;
- [docs/TZ.md](docs/TZ.md) - техническое задание;
- [docs/plan.md](docs/plan.md) - учебный план и этапы.
