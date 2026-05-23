// watcher.go — hot-reload watcher for the config file.
//
// Uses koanf's file.Provider.Watch() to detect file changes and debounces
// rapid writes into a single callback per logical change.
// The watcher goroutine stops when ctx is cancelled (returns ctx.Err()).
//
// Requirements: MVP-FND-2.5. Design: §5.2 (hot-reload).
package config

import (
	"context"
	"fmt"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// debounceWindow is the minimum quiet period after the last file event before
// invoking the onChange callback. Per MVP-FND-2.5 this must be ≥ 200 ms.
const debounceWindow = 200 * time.Millisecond

// Watch installs a file watcher on path and calls onChange whenever the file
// is written. Multiple writes within debounceWindow are coalesced into one
// callback with the final file state.
//
// Watch blocks until ctx is cancelled; it returns ctx.Err() on cancellation.
func Watch(ctx context.Context, path string, onChange func(*Config)) error {
	fp := file.Provider(path)

	// Channel to receive raw file events from the koanf watcher.
	events := make(chan struct{}, 1)

	if err := fp.Watch(func(_ interface{}, err error) {
		if err != nil {
			return
		}
		// Non-blocking send — if an event is already pending, skip.
		select {
		case events <- struct{}{}:
		default:
		}
	}); err != nil {
		return fmt.Errorf("config: starting watcher on %q: %w", path, err)
	}

	var debounce *time.Timer
	resetDebounce := func() {
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(debounceWindow, func() {
			cfg, err := load(path)
			if err != nil {
				return
			}
			onChange(cfg)
		})
	}

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return ctx.Err()
		case <-events:
			resetDebounce()
		}
	}
}

// load is a thin alias used by the watcher to reload without going through
// the exported Load (which requires an os.Stat check on the file — already
// confirmed to exist at this point).
func load(path string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: reloading %q: %w", path, err)
	}
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshalling after reload: %w", err)
	}
	return &cfg, nil
}
