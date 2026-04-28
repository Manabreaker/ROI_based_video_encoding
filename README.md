# ROI Based Video Streaming

CLI на Go для демонстрации ROI-based video streaming: входное видео используется как baseline, а отдельный ROI-вариант кодируется так, чтобы важная область кадра оставалась субъективно близкой к исходнику при меньшем битрейте.

Это не encoder-level ROI через QP-map. Текущая реализация строит маску качества средствами FFmpeg: ROI берётся из исходного кадра, вокруг него добавляется средняя зона, а периферия упрощается перед финальным H.264-кодированием.

## Что делает программа

Pipeline берётся из кода в `internal/roi/app.go`:

1. Проверяет конфиг и наличие `ffmpeg`/`ffprobe`.
2. Выбирает энкодер: `h264_nvenc`, если он доступен и выбран `--encoder auto`, иначе `libx264`.
3. Читает параметры видео через `ffprobe`.
4. Выбирает ROI: статически, из `--roi`, по центру кадра или простой motion-эвристикой.
5. Генерирует `roi_high_quality_region.mp4`.
6. Считает bitrate windows по пакетам видео через `ffprobe`.
7. Рендерит `comparison_baseline_vs_roi.mp4` с текущим битрейтом и цветными зонами.
8. Пишет JSON-отчёты и, при необходимости, считает ROI PSNR.

Baseline теперь является исходным входным видео и не перекодируется. Отдельного `baseline_uniform_low_quality.mp4` больше нет.

## Зоны качества

Правая часть comparison-видео показывает ROI output с тремя зонами:

- зелёная зона: ROI, кроп из исходного кадра;
- оранжевая зона: middle ring вокруг ROI, среднее качество;
- красная зона: outer periphery, самое сильное упрощение.

Настройки зон:

| Флаг                | Назначение                                    |
|---------------------|-----------------------------------------------|
| `--middle-margin`   | насколько расширить ROI для оранжевой зоны    |
| `--middle-scale`    | scale для средней зоны перед обратным upscale |
| `--middle-blur`     | blur для средней зоны                         |
| `--periphery-scale` | scale для красной зоны при `--fit-roi=false`  |
| `--blur`            | blur для красной зоны при `--fit-roi=false`   |
| `--roi-min-scale`   | минимальный scale кандидатов при fitting      |
| `--roi-max-blur`    | максимальный blur кандидатов при fitting      |

По умолчанию fitting включён, поэтому программа пробует несколько уровней деградации периферии и выбирает вариант около `--target-bitrate`.

## Артефакты

После запуска в `--out` появляются:

```text
roi_high_quality_region.mp4
comparison_baseline_vs_roi.mp4
roi_preview.png
bitrate_windows.json
report.json
quality_roi_psnr.json      # только если --metrics=true и метрики посчитались
```

`comparison_baseline_vs_roi.mp4` содержит:

- слева: `INPUT baseline`, то есть исходное видео;
- справа: `ROI output`;
- average/source bitrate, target/actual bitrate и размер файлов;
- `current ... kbps` по временным окнам;
- зелёную, оранжевую и красную разметку зон.

## Требования

- Go;
- FFmpeg;
- FFprobe.

### MacOS troubleshooting

По умолчанию на macOS ставится *облегченная* версия FFmpeg.
Если вы сталкиваетесь со следующей ошибкой:
```text
[AVFilterGraph @ 0xc7f020000] No such filter: 'drawtext' Error : Filter not found
```
Удалите старый ffmpeg и скачайте полную версию:
```zsh
brew uninstall ffmpeg
brew install homebrew-ffmpeg/ffmpeg/ffmpeg
```

Проверка:

```bash
go version
ffmpeg -version
ffprobe -version
```

Для хранения видео файлов используется `git lfs`, убедитесь, что он установлен и настроен.
```bash
git lfs --version
```

Скачайте файлы:
```bash
git lfs pull
```

Проект использует стандартную библиотеку Go и внешние процессы FFmpeg/FFprobe. В `go.mod` сейчас нет сторонних Go-зависимостей.

## Сборка и тесты

```bash
go build -o roi-poc ./cmd/roi
go test ./...
go test ./... -cover
go vet ./...
```

## Быстрый запуск

Пример команды для локального теста на видео из `examples`:

```bash
go build -o roi-poc ./cmd/roi

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

Открывать для сравнения:

```text
out/ball_roi/comparison_baseline_vs_roi.mp4
```

## ROI

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

Motion режим извлекает два кадра, считает разницу яркости, строит bounding box изменившихся пикселей и расширяет его через `--roi-margin`. Это простая эвристика, а не CV/ML-детектор.

## Битрейт и качество

Основной режим:

```bash
--roi-rate-control abr
--target-bitrate 1000k
```

В ABR-режиме ROI output кодируется около заданного битрейта. Для `libx264` используется two-pass, если включён `--roi-two-pass=true`; для `h264_nvenc` используется single-pass ABR.

Старое fixed-quality поведение можно включить так:

```bash
--roi-rate-control crf
--roi-crf 16
```

При `--metrics=true` программа считает ROI-crop PSNR для baseline и ROI output через FFmpeg `psnr` filter и пишет `quality_roi_psnr.json`.

## Энкодеры и GPU

Флаг:

```bash
--encoder auto
```

Поведение:

- `auto`: запускает `ffmpeg -hide_banner -encoders` через `exec.Command` и ищет `h264_nvenc` как отдельный token;
- если `h264_nvenc` найден, используется NVIDIA NVENC;
- если не найден, используется `libx264`;
- `--encoder libx264` принудительно включает CPU encoding;
- `--encoder h264_nvenc` завершится ошибкой, если FFmpeg не показывает этот энкодер.

Для NVENC preset используется:

```bash
--nvenc-preset p4
```

## HTTP preview

```bash
./roi-poc \
  --input input.mp4 \
  --out out/demo \
  --mode static \
  --serve
```

После обработки:

```text
http://localhost:8080/comparison_baseline_vs_roi.mp4
```

Адрес меняется через `--http`, например `--http :9090`.

## Docker

Docker здесь нужен не для алгоритма, а для воспроизводимого окружения: собрать Go CLI и получить runtime с FFmpeg/FFprobe без ручной установки на машине.

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

GPU в Docker требует NVIDIA runtime на хосте и доступного NVENC в FFmpeg:

```bash
docker run --rm --gpus all -v "$PWD:/work" roi-poc \
  --input examples/ball.mp4 \
  --out out/docker_nvenc \
  --encoder h264_nvenc \
  --target-bitrate 500k
```

## Структура проекта

```text
├── Dockerfile                      multi-stage build + FFmpeg runtime
├── README.md
├── docs
│   └── ...    
├── cmd
│   └── roi
│       └── main.go                 CLI entrypoint
└── internal
    └── roi
        ├── app.go                  основной orchestration pipeline
        ├── bitrate.go              bitrate windows по ffprobe packets
        ├── bitrate_test.go         
        ├── cli.go                  CLI flags
        ├── config.go               validation
        ├── config_test.go          
        ├── encode.go               ROI candidates, fitting, FFmpeg filter graph
        ├── encoder.go              выбор libx264/NVENC и encoder args
        ├── encoder_test.go         
        ├── exec.go                 
        ├── files.go                
        ├── metrics.go              ROI PSNR
        ├── metrics_test.go         
        ├── probe.go                ffprobe metadata
        ├── probe_test.go           
        ├── render.go               preview и side-by-side comparison
        ├── render_test.go          
        ├── roi.go                  static/motion ROI selection
        ├── roi_test.go             
        ├── selection.go            выбор лучшего кандидата
        ├── selection_test.go       
        ├── server.go               локальный HTTP server
        └── types.go                
```

## Используемые технологии

- Go CLI: orchestration, validation, JSON reports, unit tests.
- FFmpeg filters: `split`, `scale`, `crop`, `overlay`, `boxblur`, `drawbox`, `drawtext`, `psnr`.
- FFprobe: metadata, packet sizes, bitrate windows.
- H.264 encoders: `libx264` или `h264_nvenc`.
- Docker: воспроизводимый запуск и демонстрация.

Для текущего PoC стек уместный: большая часть тяжёлой видеообработки отдана FFmpeg, а Go держит конфигурацию, выбор кандидатов и отчёты. Лишняя работа для production streaming есть только в демонстрационной части: перебор нескольких encode-кандидатов, финальный side-by-side render и PSNR-метрики. Для офлайн-защиты это полезно; в realtime pipeline эти шаги нужно заменить на encoder-level ROI/QP-map и потоковую доставку.

## Ограничения

- Одна прямоугольная ROI.
- Motion ROI основан на разнице двух кадров.
- ROI сохраняется через mask/overlay preprocessing, а не через QP-map энкодера.
- Нет WebRTC/DASH/RTSP delivery pipeline.
- Нет object detection, saliency map и динамической ROI на каждом кадре.
