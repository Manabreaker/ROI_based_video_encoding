# ROI Based Video Streaming

CLI на Go для демонстрации ROI-based video streaming. Программа берет входное видео как baseline, создает ROI-вариант с повышенным качеством выбранной области и сохраняет side-by-side comparison, preview и JSON-отчеты.

По умолчанию ROI задается как encoder-level QP map через FFmpeg `addroi`: в кадры добавляются ROI side data с `qoffset`, а энкодер перераспределяет качество без предварительного размытия пикселей. Старый mask-based preprocessing тоже доступен через `--roi-control mask`.

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
roi-control: qp-map
roi-qoffset: -0.30
roi-middle-qoffset: -0.10
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

Для более точной QP-map разметки можно задавать ROI блоками сетки:

```yaml
mode: blocks
roi-control: qp-map
roi-block-size: 64
roi-blocks:
  - col: 4
    row: 2
    w: 2
    h: 2
    qoffset: -0.40
  - col: 3
    row: 1
    qoffset: -0.15
```

`col` и `row` - индексы блоков, не пиксели. `w` и `h` задаются тоже в блоках; если их не указать, используется один блок 64x64. Пример лежит в [config/qp_block_map_example.yaml](config/qp_block_map_example.yaml).

## ROI block painter UI

Для ручной разметки блочной QP-map можно запустить отдельный локальный UI:

```bash
go run ./cmd/roi-map-ui \
  --input examples/ball.mp4 \
  --config-out config/roi_blocks_generated.yaml \
  --out out/roi_blocks_generated \
  --target-bitrate 500k
```

Программа поднимает маленький localhost-сервер, открывает страницу в браузере и позволяет рисовать блоки поверх видео. Кнопка `Confirm` записывает готовый YAML config, который затем запускается обычным encoder CLI:

```bash
go run ./cmd/roi --config config/roi_blocks_generated.yaml
```

По умолчанию палитра задает `qoffset`: green `-0.40`, orange `-0.25`, yellow `-0.10`, red `+0.15`. Карта статична для всего видео; ползунок нужен только чтобы выбрать удобный кадр для рисования.

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
- разметку ROI: зеленая основная ROI, оранжевая middle ring; в `--roi-control mask` дополнительно рисуется красная periphery.

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

Режим ROI control:

```bash
--roi-control qp-map
--roi-qoffset -0.30
--roi-middle-qoffset -0.10
```

`qp-map` использует FFmpeg `addroi` и передает энкодеру ROI side data. Отрицательный `qoffset` просит энкодер снизить QP внутри области, то есть сохранить там больше качества. Для `libx264` программа включает `-aq-mode 1`, потому что x264 использует ROI side data только с adaptive quantization; для `h264_nvenc` включается `-spatial-aq 1`.

Кроме прямоугольной ROI можно использовать блочную QP-map:

```bash
--mode blocks
--roi-block-size 64
--roi-blocks "4,2,2,2,-0.40;3,1,-0.15"
```

Кадр делится на сетку 64x64, блоки нумеруются с нуля слева направо и сверху вниз. Формат флага: `col,row,qoffset` для одного блока или `col,row,w,h,qoffset` для прямоугольника из блоков. Блоки не должны пересекаться; `qoffset` остается в диапазоне `[-1,1]`.

Старый preprocessing-режим:

```bash
--roi-control mask
```

В этом режиме ROI берется из исходного кадра, middle ring ухудшается мягче, а периферия ухудшается сильнее через scale/blur перед финальным encode.

Основной режим по умолчанию:

```bash
--roi-rate-control abr
--target-bitrate 1000k
```

В ABR-режиме ROI output кодируется около заданного битрейта. Для `libx264` используется two-pass, если включен `--roi-two-pass=true`; для `h264_nvenc` используется single-pass ABR.

Fixed-quality режим:

```bash
--roi-rate-control crf
--roi-crf 16
```

Для `--roi-control mask` по умолчанию включен fitting:

```bash
--fit-roi=true
```

В mask-режиме программа не проходит всю лестницу качества линейно. Она использует interpolation search: сначала измеряет несколько опорных вариантов, затем по измеренному bitrate прыгает к кандидату, который должен быть ближе к `--target-bitrate`, и при необходимости проверяет соседние уровни. Лимит probe-ов задается через `--fit-iterations`. В `qp-map` режиме fitting scale/blur не нужен: bitrate задается rate-control, а распределение качества задается QP offsets.

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
| `--encoder`            | `auto`       | `auto`, `libx264` или `h264_nvenc`                             |
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
- если `h264_nvenc` найден, используется NVIDIA NVENC;
- если NVENC недоступен, используется `libx264`;
- `--encoder libx264` принудительно включает CPU encoding;
- `--encoder h264_nvenc` завершится ошибкой, если FFmpeg не поддерживает этот энкодер.

NVENC preset задается так:

```bash
--nvenc-preset p4
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

### NVENC не выбирается

Проверьте, что FFmpeg видит энкодер:

```bash
ffmpeg -hide_banner -encoders | grep h264_nvenc
```

Если энкодера нет, используйте CPU:

```bash
--encoder libx264
```

### NVENC падает на UHD comparison

Для 3840x2160 входа side-by-side comparison имеет ширину 7680 пикселей. Некоторые NVENC H.264 устройства принимают максимум 4096 пикселей по ширине и возвращают ошибку:

```text
Width 7680 exceeds 4096
```

Программа автоматически уменьшает только `comparison_baseline_vs_roi.mp4` до ширины 4096 при `h264_nvenc`. Основной `roi_high_quality_region.mp4` остается в исходном разрешении, поэтому измеренный ROI bitrate и QP-map не меняются.

## Структура документации

В этом репозитории используется практичная схема:

- [README.md](README.md) - как собрать, запустить и проверить результат;
- [docs/overview.md](docs/overview.md) - обзор проекта, архитектуры, pipeline и ограничений;
- [docs/research.md](docs/research.md) - исследовательский контекст;
- [docs/TZ.md](docs/TZ.md) - техническое задание;
- [docs/plan.md](docs/plan.md) - учебный план и этапы.
