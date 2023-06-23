package core

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

type customFormatter struct {
	logrus.TextFormatter
}

func (f *customFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	// var levelColor int
	// switch entry.Level {
	// case logrus.DebugLevel, logrus.TraceLevel:
	// 	levelColor = 42 // green highlight
	// case logrus.WarnLevel:
	// 	levelColor = 33 // yellow
	// case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
	// 	levelColor = 41 // red highlight
	// default:
	// 	levelColor = 36 // blue
	// }
	//[]byte(fmt.Sprintf("[%s] - \x1b[%dm%s\x1b[0m - %s\n", entry.Time.Format(f.TimestampFormat), levelColor, strings.ToUpper(entry.Level.String()), entry.Message))

	return []byte(fmt.Sprintf("[%s][%s] \t%s\n", entry.Time.Format(f.TimestampFormat), strings.ToUpper(entry.Level.String()), entry.Message)), nil
}

func InitLogger(isVerbose, isDebug bool) {
	logrus.SetFormatter(&customFormatter{logrus.TextFormatter{
		FullTimestamp:          true,
		TimestampFormat:        "2006-01-02 15:04:05",
		ForceColors:            true,
		DisableLevelTruncation: true,
	}})

	if isVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if isDebug {
		logrus.SetOutput(io.MultiWriter(os.Stdout))
		logrus.SetLevel(logrus.TraceLevel)
		logrus.SetReportCaller(true)
	} else {
		f, err := os.OpenFile("./logs.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("Failed to create logsfile: ./logs.txt")
			panic(err)
		}

		logrus.SetOutput(io.MultiWriter(f, os.Stdout))
		logrus.SetLevel(logrus.DebugLevel)
	}
}
