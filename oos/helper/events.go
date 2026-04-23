package helper

import "sync"

type BoardEvent struct {
	ScreenID string
	Action   string
	JSON     []byte 
	Result   string
	Error    string 
}

var (
	boardEventMu       sync.RWMutex
	boardEventHandlers = map[string]func(BoardEvent){}
)

func AddBoardEventHandler(name string, fn func(BoardEvent)) {
	boardEventMu.Lock()
	defer boardEventMu.Unlock()
	boardEventHandlers[name] = fn
}

func RemoveBoardEventHandler(name string) {
	boardEventMu.Lock()
	defer boardEventMu.Unlock()
	delete(boardEventHandlers, name)
}

func FireBoardEvent(ev BoardEvent) {
	boardEventMu.RLock()
	handlers := make([]func(BoardEvent), 0, len(boardEventHandlers))
	for _, fn := range boardEventHandlers {
		handlers = append(handlers, fn)
	}
	boardEventMu.RUnlock()

	for _, fn := range handlers {
		fn(ev)
	}
}
