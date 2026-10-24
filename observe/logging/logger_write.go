package logging

import (
	"fmt"
	"io"
	"log"
	"sync"
)

func newLineLogger(writer io.Writer) *log.Logger {
	if writer == nil {
		return nil
	}
	return log.New(writer, "", log.LstdFlags)
}

func writeLogLine(logger *log.Logger, args ...any) {
	if logger == nil {
		log.Println(args...)
		return
	}
	logger.Println(args...)
}

func writeRawLine(mu *sync.Mutex, writer io.Writer, line string) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	if writer == nil {
		_, _ = fmt.Fprintln(log.Writer(), line)
		return
	}
	_, _ = fmt.Fprintln(writer, line)
}
