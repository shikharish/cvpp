package progress

import (
	"fmt"
	"sync"
	"time"
)

var (
	mu    sync.Mutex
	sinks = map[int]func(string){}
	next  int
)

func AddSink(sink func(string)) func() {
	mu.Lock()
	defer mu.Unlock()
	id := next
	next++
	sinks[id] = sink
	return func() {
		mu.Lock()
		defer mu.Unlock()
		delete(sinks, id)
	}
}

func Logf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message)
	fmt.Println(line)

	mu.Lock()
	listeners := make([]func(string), 0, len(sinks))
	for _, sink := range sinks {
		listeners = append(listeners, sink)
	}
	mu.Unlock()
	for _, sink := range listeners {
		sink(line)
	}
}
