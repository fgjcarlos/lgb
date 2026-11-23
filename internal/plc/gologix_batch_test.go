package plc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/fgjcarlos/lgb/internal/config"
	errs "github.com/fgjcarlos/lgb/internal/errors"
	"github.com/fgjcarlos/lgb/internal/plc"
)

// batchFakeClient simulates a PLC holding the values in tags. ReadList mirrors
// gologix: one missing tag fails the whole multi-service request.
type batchFakeClient struct {
	mu        sync.Mutex
	tags      map[string]any
	listErr   error // forced ReadList error (e.g. transport failure)
	listCalls int
	readCalls int
	listed    []string
}

func (f *batchFakeClient) Connect() error          { return nil }
func (f *batchFakeClient) Disconnect() error       { return nil }
func (f *batchFakeClient) Write(string, any) error { return nil }
func (f *batchFakeClient) Connected() bool         { return true }

func (f *batchFakeClient) Read(tag string, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	v, ok := f.tags[tag]
	if !ok {
		return fmt.Errorf("tag %s not in map", tag)
	}
	reflect.ValueOf(data).Elem().Set(reflect.ValueOf(v))
	return nil
}

func (f *batchFakeClient) ReadList(names []string, types []any, elements []int) ([]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	f.listed = append(f.listed, names...)
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]any, len(names))
	for i, n := range names {
		v, ok := f.tags[n]
		if !ok {
			return nil, fmt.Errorf("problem reading %v. Status Path Destination Unknown", n)
		}
		out[i] = v
	}
	return out, nil
}

func newBatchDriver(t *testing.T, fake *batchFakeClient) plc.Driver {
	t.Helper()
	d := plc.NewDriverWithClient(config.PLC{Name: "batch", Address: "127.0.0.1"}, fake)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return d
}

// TestGologixDriver_ReadMulti_BatchesScalarTags verifies N scalar tags are read
// with a single multi-service request and no per-tag reads.
func TestGologixDriver_ReadMulti_BatchesScalarTags(t *testing.T) {
	t.Parallel()
	fake := &batchFakeClient{tags: map[string]any{
		"B": true, "I": int16(42), "D": int32(-7), "F": float32(3.5), "U": uint8(9),
	}}
	d := newBatchDriver(t, fake)

	var b bool
	var i int16
	var di int32
	var f float32
	var u uint8
	errsOut, err := d.ReadMulti([]string{"B", "I", "D", "F", "U"}, []any{&b, &i, &di, &f, &u})
	if err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}
	for k, e := range errsOut {
		if e != nil {
			t.Errorf("tag[%d] err = %v, want nil", k, e)
		}
	}
	if !b || i != 42 || di != -7 || f != 3.5 || u != 9 {
		t.Errorf("values = %v %v %v %v %v", b, i, di, f, u)
	}
	if fake.listCalls != 1 || fake.readCalls != 0 {
		t.Errorf("ReadList calls = %d, Read calls = %d; want 1, 0", fake.listCalls, fake.readCalls)
	}
}

// TestGologixDriver_ReadMulti_BadTagDegradesOnlyThatTag verifies that when the
// batch fails because one tag is missing, the driver falls back to per-tag
// reads and only the missing tag reports an error.
func TestGologixDriver_ReadMulti_BadTagDegradesOnlyThatTag(t *testing.T) {
	t.Parallel()
	fake := &batchFakeClient{tags: map[string]any{"A": int32(1), "C": int32(3)}}
	d := newBatchDriver(t, fake)

	var a, b, c int32
	errsOut, err := d.ReadMulti([]string{"A", "Missing", "C"}, []any{&a, &b, &c})
	if err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}
	if errsOut[0] != nil || errsOut[2] != nil {
		t.Errorf("good tags errs = %v, %v; want nil", errsOut[0], errsOut[2])
	}
	if !errors.Is(errsOut[1], errs.ErrPLCRead) {
		t.Errorf("missing tag err = %v, want ErrPLCRead", errsOut[1])
	}
	if a != 1 || c != 3 {
		t.Errorf("values = %d, %d; want 1, 3", a, c)
	}
	if fake.listCalls != 1 || fake.readCalls != 3 {
		t.Errorf("ReadList calls = %d, Read calls = %d; want 1, 3", fake.listCalls, fake.readCalls)
	}
}

// TestGologixDriver_ReadMulti_ConnectionErrorSkipsFallback verifies that a
// transport failure marks every batched tag failed without N extra round-trips.
func TestGologixDriver_ReadMulti_ConnectionErrorSkipsFallback(t *testing.T) {
	t.Parallel()
	fake := &batchFakeClient{tags: map[string]any{"A": int32(1)}, listErr: io.EOF}
	d := newBatchDriver(t, fake)

	var a, b int32
	errsOut, err := d.ReadMulti([]string{"A", "B"}, []any{&a, &b})
	if err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}
	for k, e := range errsOut {
		if !errors.Is(e, errs.ErrPLCConnect) {
			t.Errorf("tag[%d] err = %v, want ErrPLCConnect", k, e)
		}
	}
	if fake.readCalls != 0 {
		t.Errorf("Read calls = %d, want 0", fake.readCalls)
	}
}

// TestGologixDriver_ReadMulti_UnbatchableFallsBackToReadTag verifies that
// destinations the batch path cannot decode (strings, slices) use ReadTag.
func TestGologixDriver_ReadMulti_UnbatchableFallsBackToReadTag(t *testing.T) {
	t.Parallel()
	fake := &batchFakeClient{tags: map[string]any{
		"S": "hello", "Arr": []int32{1, 2}, "N": int32(5),
	}}
	d := newBatchDriver(t, fake)

	var s string
	arr := make([]int32, 2)
	var n int32
	errsOut, err := d.ReadMulti([]string{"S", "Arr", "N"}, []any{&s, &arr, &n})
	if err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}
	for k, e := range errsOut {
		if e != nil {
			t.Errorf("tag[%d] err = %v, want nil", k, e)
		}
	}
	if s != "hello" || n != 5 || len(arr) != 2 || arr[1] != 2 {
		t.Errorf("values = %q %v %d", s, arr, n)
	}
	if !reflect.DeepEqual(fake.listed, []string{"N"}) || fake.readCalls != 2 {
		t.Errorf("batched = %v, Read calls = %d; want [N], 2", fake.listed, fake.readCalls)
	}
}

// TestGologixDriver_ReadMulti_TypeMismatchFailsOnlyThatTag verifies that a
// PLC value whose type does not match the destination degrades only that tag.
func TestGologixDriver_ReadMulti_TypeMismatchFailsOnlyThatTag(t *testing.T) {
	t.Parallel()
	fake := &batchFakeClient{tags: map[string]any{"A": int16(1), "B": int32(2)}}
	d := newBatchDriver(t, fake)

	var a, b int32
	errsOut, err := d.ReadMulti([]string{"A", "B"}, []any{&a, &b})
	if err != nil {
		t.Fatalf("ReadMulti: %v", err)
	}
	if !errors.Is(errsOut[0], errs.ErrPLCRead) {
		t.Errorf("mismatched tag err = %v, want ErrPLCRead", errsOut[0])
	}
	if errsOut[1] != nil || b != 2 {
		t.Errorf("good tag err = %v, value = %d", errsOut[1], b)
	}
}
