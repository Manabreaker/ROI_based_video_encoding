package roi

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// copyFile copies the selected candidate to the stable output path.
func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	return nil
}

// artifactFor collects file size and container-level bitrate for reports.
func artifactFor(path string) Artifact {
	st, err := os.Stat(path)
	if err != nil {
		return Artifact{Path: path}
	}

	artifact := Artifact{
		Path:      path,
		SizeBytes: st.Size(),
	}

	if strings.HasSuffix(strings.ToLower(path), ".mp4") {
		info, err := probeVideo(path)
		if err == nil && info.Duration > 0 {
			artifact.BitrateKbps = float64(st.Size()*8) / info.Duration / 1000.0
		}
	}

	return artifact
}

// formatBytes renders file sizes for overlays and logs.
func formatBytes(size int64) string {
	if size <= 0 {
		return "size n/a"
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024.0)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024.0*1024.0))
	}

	return fmt.Sprintf("%.2f GB", float64(size)/(1024.0*1024.0*1024.0))
}
