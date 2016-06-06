package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

type TestLoggerJSON struct {
	*json.Encoder
	delay time.Duration
}

func (l *TestLoggerJSON) Log(m *Message) error {
	if l.delay > 0 {
		time.Sleep(l.delay)
	}
	return l.Encode(m)
}

func (l *TestLoggerJSON) Close() error { return nil }

func (l *TestLoggerJSON) Name() string { return "json" }

type TestLoggerText struct {
	*bytes.Buffer
}

func (l *TestLoggerText) Log(m *Message) error {
	_, err := l.WriteString(m.ContainerID + " " + m.Source + " " + string(m.Line) + "\n")
	return err
}

func (l *TestLoggerText) Close() error { return nil }

func (l *TestLoggerText) Name() string { return "text" }

func TestCopier(t *testing.T) {
	stdoutLine := "Line that thinks that it is log line from docker stdout"
	stderrLine := "Line that thinks that it is log line from docker stderr"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	for i := 0; i < 30; i++ {
		if _, err := stdout.WriteString(stdoutLine + "\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := stderr.WriteString(stderrLine + "\n"); err != nil {
			t.Fatal(err)
		}
	}

	var jsonBuf bytes.Buffer

	jsonLog := &TestLoggerJSON{Encoder: json.NewEncoder(&jsonBuf)}

	cid := "a7317399f3f857173c6179d44823594f8294678dea9999662e5c625b5a1c7657"
	c := NewCopier(cid,
		map[string]io.Reader{
			"stdout": &stdout,
			"stderr": &stderr,
		},
		jsonLog)
	c.Run()
	wait := make(chan struct{})
	go func() {
		c.Wait()
		close(wait)
	}()
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Copier failed to do its work in 1 second")
	case <-wait:
	}
	dec := json.NewDecoder(&jsonBuf)
	for {
		var msg Message
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if msg.Source != "stdout" && msg.Source != "stderr" {
			t.Fatalf("Wrong Source: %q, should be %q or %q", msg.Source, "stdout", "stderr")
		}
		if msg.ContainerID != cid {
			t.Fatalf("Wrong ContainerID: %q, expected %q", msg.ContainerID, cid)
		}
		if msg.Source == "stdout" {
			if string(msg.Line) != stdoutLine {
				t.Fatalf("Wrong Line: %q, expected %q", msg.Line, stdoutLine)
			}
		}
		if msg.Source == "stderr" {
			if string(msg.Line) != stderrLine {
				t.Fatalf("Wrong Line: %q, expected %q", msg.Line, stderrLine)
			}
		}
	}
}

func TestCopierSlow(t *testing.T) {
	stdoutLine := "Line that thinks that it is log line from docker stdout"
	var stdout bytes.Buffer
	for i := 0; i < 30; i++ {
		if _, err := stdout.WriteString(stdoutLine + "\n"); err != nil {
			t.Fatal(err)
		}
	}

	var jsonBuf bytes.Buffer
	//encoder := &encodeCloser{Encoder: json.NewEncoder(&jsonBuf)}
	jsonLog := &TestLoggerJSON{Encoder: json.NewEncoder(&jsonBuf), delay: 100 * time.Millisecond}

	cid := "a7317399f3f857173c6179d44823594f8294678dea9999662e5c625b5a1c7657"
	c := NewCopier(cid, map[string]io.Reader{"stdout": &stdout}, jsonLog)
	c.Run()
	wait := make(chan struct{})
	go func() {
		c.Wait()
		close(wait)
	}()
	<-time.After(150 * time.Millisecond)
	c.Close()
	select {
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("failed to exit in time after the copier is closed")
	case <-wait:
	}
}

type BenchmarkLoggerDummy struct {
}

func (l *BenchmarkLoggerDummy) Log(m *Message) error { return nil }

func (l *BenchmarkLoggerDummy) Close() error { return nil }

func (l *BenchmarkLoggerDummy) Name() string { return "dummy" }

func BenchmarkCopier64(b *testing.B) {
	benchmarkCopier(b, 1<<6)
}
func BenchmarkCopier128(b *testing.B) {
	benchmarkCopier(b, 1<<7)
}
func BenchmarkCopier256(b *testing.B) {
	benchmarkCopier(b, 1<<8)
}
func BenchmarkCopier512(b *testing.B) {
	benchmarkCopier(b, 1<<9)
}
func BenchmarkCopier1K(b *testing.B) {
	benchmarkCopier(b, 1<<10)
}
func BenchmarkCopier2K(b *testing.B) {
	benchmarkCopier(b, 1<<11)
}
func BenchmarkCopier4K(b *testing.B) {
	benchmarkCopier(b, 1<<12)
}
func BenchmarkCopier8K(b *testing.B) {
	benchmarkCopier(b, 1<<13)
}
func BenchmarkCopier16K(b *testing.B) {
	benchmarkCopier(b, 1<<14)
}
func BenchmarkCopier32K(b *testing.B) {
	benchmarkCopier(b, 1<<15)
}
func BenchmarkCopier64K(b *testing.B) {
	benchmarkCopier(b, 1<<16)
}
func BenchmarkCopier128K(b *testing.B) {
	benchmarkCopier(b, 1<<17)
}
func BenchmarkCopier256K(b *testing.B) {
	benchmarkCopier(b, 1<<18)
}

func piped(b *testing.B, iterations int, delay time.Duration, buf []byte) io.Reader {
	r, w, err := os.Pipe()
	if err != nil {
		b.Fatal(err)
		return nil
	}
	go func() {
		for i := 0; i < iterations; i++ {
			time.Sleep(delay)
			if n, err := w.Write(buf); err != nil || n != len(buf) {
				if err != nil {
					b.Fatal(err)
				}
				b.Fatal(fmt.Errorf("short write"))
			}
		}
		w.Close()
	}()
	return r
}

func benchmarkCopier(b *testing.B, length int) {
	b.StopTimer()
	buf := []byte{'A'}
	for len(buf) < length {
		buf = append(buf, buf...)
	}
	buf = append(buf[:length-1], []byte{'\n'}...)
	b.StartTimer()
	cid := "a7317399f3f857173c6179d44823594f8294678dea9999662e5c625b5a1c7657"
	for i := 0; i < b.N; i++ {
		c := NewCopier(cid,
			map[string]io.Reader{
				"buffer": piped(b, 10, time.Nanosecond, buf),
			},
			&BenchmarkLoggerDummy{})
		c.Run()
		c.Wait()
		c.Close()
	}
}
