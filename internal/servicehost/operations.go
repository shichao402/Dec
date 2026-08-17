package servicehost

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
)

type operationState struct {
	meta        *servicev1.ActiveOperation
	history     []*servicev1.WatchOperationResponse
	subscribers map[chan *servicev1.WatchOperationResponse]struct{}
	done        bool
}

const (
	// finishedStateTTL 是操作结束后仍保留其状态/history 供「迟到的旁观者」读取的时长；
	// 到期后从 byProject 删除，避免每个见过的 project root 永久驻留一份 history。
	finishedStateTTL = 5 * time.Minute
	// maxHistoryEvents 限制单个操作 history 的事件条数，防止极长操作无界累积；
	// 超出后丢弃最旧事件（迟到旁观者可能看不到最早的进度行，可接受）。
	maxHistoryEvents = 4096
)

type operationBroker struct {
	mu          sync.Mutex
	byProject   map[string]*operationState
	nextID      uint64
	finishedTTL time.Duration
	afterFunc   func(time.Duration, func()) *time.Timer
}

func newOperationBroker() *operationBroker {
	return &operationBroker{
		byProject:   make(map[string]*operationState),
		finishedTTL: finishedStateTTL,
		afterFunc:   time.AfterFunc,
	}
}

// appendHistory 追加事件并在超过上限时丢弃最旧的，保持 history 有界。
func appendHistory(history []*servicev1.WatchOperationResponse, message *servicev1.WatchOperationResponse) []*servicev1.WatchOperationResponse {
	history = append(history, message)
	if len(history) > maxHistoryEvents {
		drop := len(history) - maxHistoryEvents
		history = append(history[:0], history[drop:]...)
	}
	return history
}

func projectKey(root string) string {
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return root
}

func (b *operationBroker) start(projectRoot, operation, clientID, facade string) (*operationState, error) {
	key := projectKey(projectRoot)
	b.mu.Lock()
	defer b.mu.Unlock()
	if current := b.byProject[key]; current != nil && !current.done {
		return nil, fmt.Errorf("project busy: %s 正由 %s(%s) 执行 %s",
			key, current.meta.Facade, current.meta.ClientId, current.meta.Operation)
	}
	b.nextID++
	state := &operationState{
		meta: &servicev1.ActiveOperation{
			Active:          true,
			OperationId:     fmt.Sprintf("op-%d-%d", time.Now().UnixNano(), b.nextID),
			Operation:       operation,
			ClientId:        clientID,
			Facade:          facade,
			StartedAtUnixMs: time.Now().UnixMilli(),
		},
		subscribers: make(map[chan *servicev1.WatchOperationResponse]struct{}),
	}
	b.byProject[key] = state
	return state, nil
}

func (b *operationBroker) active(projectRoot string) *servicev1.ActiveOperation {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.byProject[projectKey(projectRoot)]
	if state == nil || state.done {
		return &servicev1.ActiveOperation{}
	}
	return cloneActive(state.meta)
}

// firstActive 返回任意 project 上第一个未结束的活跃操作；无则 nil。
func (b *operationBroker) firstActive() *servicev1.ActiveOperation {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, state := range b.byProject {
		if state == nil || state.done || state.meta == nil || !state.meta.Active {
			continue
		}
		return cloneActive(state.meta)
	}
	return nil
}

func (b *operationBroker) publish(projectRoot string, message *servicev1.WatchOperationResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.byProject[projectKey(projectRoot)]
	if state == nil || state.done {
		return
	}
	message.OperationId = state.meta.OperationId
	if message.Active == nil {
		message.Active = cloneActive(state.meta)
	}
	state.history = appendHistory(state.history, message)
	for ch := range state.subscribers {
		select {
		case ch <- message:
		default:
			// 旁观方不能拖慢权威操作；慢消费者下一次刷新可读最终状态。
		}
	}
}

func (b *operationBroker) finish(projectRoot string, message *servicev1.WatchOperationResponse) {
	key := projectKey(projectRoot)
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.byProject[key]
	if state == nil || state.done {
		return
	}
	message.OperationId = state.meta.OperationId
	message.Active = cloneActive(state.meta)
	message.Done = true
	state.history = appendHistory(state.history, message)
	state.done = true
	for ch := range state.subscribers {
		select {
		case ch <- message:
		default:
		}
		close(ch)
	}
	state.subscribers = nil
	// 保留已结束状态给「迟到的旁观者」读取 history；下一次 start 会覆盖同 project 条目。
	// 若在 TTL 内没有新操作覆盖，则到期删除，避免每个 project root 永久驻留一份 history。
	b.scheduleCleanup(key, state)
}

// scheduleCleanup 在 finishedTTL 后删除仍为该已结束 state 的 byProject 条目。
// 若期间被新 start 覆盖（cur != state）或已被替换，则不动。
func (b *operationBroker) scheduleCleanup(key string, state *operationState) {
	if b.finishedTTL <= 0 || b.afterFunc == nil {
		return
	}
	b.afterFunc(b.finishedTTL, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.byProject[key]; ok && cur == state && cur.done {
			delete(b.byProject, key)
		}
	})
}

func (b *operationBroker) subscribe(projectRoot, operationID string) ([]*servicev1.WatchOperationResponse, <-chan *servicev1.WatchOperationResponse, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.byProject[projectKey(projectRoot)]
	if state == nil {
		return nil, nil, nil, fmt.Errorf("project 当前没有活跃操作")
	}
	if operationID != "" && operationID != state.meta.OperationId {
		return nil, nil, nil, fmt.Errorf("操作 %s 不存在或已被新操作替代", operationID)
	}
	history := append([]*servicev1.WatchOperationResponse(nil), state.history...)
	if state.done {
		return history, nil, func() {}, nil
	}
	ch := make(chan *servicev1.WatchOperationResponse, 256)
	state.subscribers[ch] = struct{}{}
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := state.subscribers[ch]; ok {
			delete(state.subscribers, ch)
			close(ch)
		}
	}
	return history, ch, cancel, nil
}

func cloneActive(in *servicev1.ActiveOperation) *servicev1.ActiveOperation {
	if in == nil {
		return &servicev1.ActiveOperation{}
	}
	return &servicev1.ActiveOperation{
		Active:          in.Active,
		OperationId:     in.OperationId,
		Operation:       in.Operation,
		ClientId:        in.ClientId,
		Facade:          in.Facade,
		StartedAtUnixMs: in.StartedAtUnixMs,
	}
}
