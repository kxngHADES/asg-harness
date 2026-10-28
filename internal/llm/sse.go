package llm

import (
	"bufio"
	"io"
	"strings"
)

// readSSE calls fn with the event name and data payload of each server-sent event.
func readSSE(r io.Reader, fn func(event, data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var event string
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		err := fn(event, data.String())
		event = ""
		data.Reset()
		return err
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line[5:], " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}
