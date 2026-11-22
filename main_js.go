//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
	"time"
)

const playgroundExpireInterval = time.Second

func main() {
	store := NewStore()

	exec := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return js.Null()
		}
		r, ok := runInline(store, args[0].String())
		if !ok {
			return js.Null()
		}
		return map[string]any{
			"text": formatReply(r),
			"resp": string(r.encode()),
		}
	})
	js.Global().Set("respkv", map[string]any{"exec": exec})

	store.RunExpiration(context.Background(), playgroundExpireInterval)
}
