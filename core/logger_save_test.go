package core

import (
	"os"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestInitLoggerSaveLog(t *testing.T) {
	logger := logrus.StandardLogger()
	out, formatter, level := logger.Out, logger.Formatter, logger.Level
	t.Cleanup(func() {
		logger.SetOutput(out)
		logger.SetFormatter(formatter)
		logger.SetLevel(level)
	})

	for _, save := range []bool{false, true} {
		t.Chdir(t.TempDir())
		InitLogger(false, false, false, save, LogFormatText)
		logger.SetOutput(out) // release ./logs.txt before the temp dir is removed
		_, err := os.Stat("logs.txt")
		if created := err == nil; created != save {
			t.Fatalf("save_log=%v: logs.txt created = %v", save, created)
		}
	}
}
