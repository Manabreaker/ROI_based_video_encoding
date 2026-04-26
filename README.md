# ROI Video Streaming PoC на Go

PoC для Этапа 3: программа берёт входное видео, выбирает область интереса, деградирует периферию и оставляет ROI в высоком качестве.

Это минимально демонстрирует идею ROI-based streaming перед интеграцией настоящих QP/ROI-map энкодеров.

## Что создаётся на выходе

После запуска в папке результата будут файлы:

```text
baseline_uniform_low_quality.mp4
roi_high_quality_region.mp4
comparison_baseline_vs_roi.mp4
roi_preview.png
report.json
```

Описание:

- `baseline_uniform_low_quality.mp4` — всё видео равномерно ухудшено.
- `roi_high_quality_region.mp4` — периферия ухудшена, ROI оставлена из исходника.
- `comparison_baseline_vs_roi.mp4` — сравнение side-by-side для защиты.
- `roi_preview.png` — кадр с выделенной ROI.
- `report.json` — параметры запуска, ROI, размеры файлов и примерный битрейт.

## Требования

Нужно установить:

- Go 1.22+
- FFmpeg
- FFprobe

Проверка:

```bash
go version
ffmpeg -version
ffprobe -version
```

## Сборка

```bash
go build -o roi-poc ./cmd/roi
```

## Быстрый запуск на тестовом видео

```bash
bash scripts/make_sample.sh
go build -o roi-poc ./cmd/roi-poc
./roi-poc --input sample_motion.mp4 --out demo_out --mode motion
```

Главный файл для показа на защите:

```text
demo_out/comparison_baseline_vs_roi.mp4
```

## Запуск со статической ROI

Можно задать ROI в пикселях:

```bash
./roi-poc \
  --input input.mp4 \
  --out demo_out_static \
  --mode static \
  --roi 640,300,520,360
```

Можно задать ROI долями кадра от `0` до `1`:

```bash
./roi-poc \
  --input input.mp4 \
  --out demo_out_static \
  --mode static \
  --roi 0.30,0.20,0.40,0.45
```

Формат:

```text
x,y,w,h
```

Где:

- `x` — координата левого верхнего угла ROI по X;
- `y` — координата левого верхнего угла ROI по Y;
- `w` — ширина ROI;
- `h` — высота ROI.

## Запуск с автоматическим motion ROI

```bash
./roi-poc \
  --input input.mp4 \
  --out demo_out_motion \
  --mode motion \
  --motion-window 0.7 \
  --motion-threshold 34
```

Motion ROI работает просто:

1. программа извлекает два кадра;
2. считает разницу яркости;
3. строит bounding box изменившейся области;
4. расширяет ROI через `--roi-margin`.

Это не production CV-модель, а минимальная проверка технической реализуемости.

## Локальная демонстрация через HTTP

```bash
./roi-poc \
  --input input.mp4 \
  --out demo_out \
  --mode static \
  --serve
```

Открыть в браузере:

```text
http://localhost:8080/comparison_baseline_vs_roi.mp4
```

## Полезные параметры

```bash
--periphery-scale 0.42
```

Насколько сильно понизить детализацию периферии. Чем меньше число, тем сильнее деградация.

```bash
--blur 2
```

Сила blur для периферии.

```bash
--crf 23
```

CRF финального H.264 файла. Меньше — лучше качество и больше размер.

```bash
--preset veryfast
```

Preset x264.

```bash
--roi-margin 0.18
```

Расширение автоматически найденной motion ROI.

```bash
--keep-temp
```

Не удалять временные кадры, которые используются для поиска движения.

## Docker

Сборка:

```bash
docker build -t roi-poc .
```

Запуск:

```bash
docker run --rm -v "$PWD:/work" roi-poc \
  --input sample_motion.mp4 \
  --out demo_out \
  --mode motion
```

## Как защищать PoC

1. Показать `roi_preview.png`: ROI найдена или задана.
2. Показать `baseline_uniform_low_quality.mp4`: обычное равномерное ухудшение.
3. Показать `roi_high_quality_region.mp4`: ROI остаётся чёткой, периферия хуже.
4. Показать `comparison_baseline_vs_roi.mp4`: основной side-by-side результат.
5. Открыть `report.json`: параметры эксперимента и размеры файлов.

## Что говорить на защите

Этот PoC показывает техническую реализуемость идеи ROI-based video streaming.

В нём уже есть:

- входной видеопоток;
- автоматическая или ручная ROI;
- разное качество внутри кадра;
- baseline для сравнения;
- демонстрационное видео;
- отчёт с параметрами запуска.

Ограничение: это mask-based PoC, а не настоящая интеграция с QP-map энкодером.

На следующем этапе этот ROI-блок можно использовать как источник карты качества для:

- SVT-AV1 RoiMapFile / QP Offset Map;
- Kvazaar `--roi`;
- libaom `AOME_SET_ROI_MAP`.