package prompter

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/AlecAivazis/survey/v2/terminal"
)

type fuzzFileReader struct {
	*bytes.Reader
}

func (r fuzzFileReader) Fd() uintptr {
	return 0
}

type fuzzFileWriter struct {
	*bytes.Buffer
}

func (w fuzzFileWriter) Fd() uintptr {
	return 0
}

func TestSurveyInputDoesNotEmitUnexpectedEscapeSequences(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "double escape", input: []byte("abc\x1b\x1b\r")},
		{name: "unknown meta key", input: []byte("abc\x1bq\r")},
		{name: "interrupted csi sequence", input: []byte("\x1b[00\x1bX")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertSurveyReaderAccepts(t, tt.input)
		})
	}
}

func FuzzSurveyInput(f *testing.F) {
	f.Add([]byte("See how this crashes:\x1bf\r"))
	f.Add([]byte("abc\x1b\x1b\r"))
	f.Add([]byte("abc\x1bq\r"))
	f.Add([]byte("\x1b[00\x1bX"))
	f.Add([]byte("abc\x17\r"))
	f.Add([]byte("abc\x1b\x7f\r"))
	f.Add([]byte("abc\x1b[3;3~\r"))
	f.Add([]byte("abc\x1b[127;3~\r"))
	f.Add([]byte("abc\x1b[8;3~\r"))
	f.Add([]byte("abc\x1b[127;3u\r"))
	f.Add([]byte("abc\x1b[8;3u\r"))
	f.Add([]byte("abc\x1b[3;3u\r"))
	f.Add([]byte("abc\x1b[27;3;127~\r"))
	f.Add([]byte("abc\x1b[27;3;100~\r"))
	f.Add([]byte("abc\x1b[100;3u\r"))
	f.Add([]byte("abc\x1b[57426;3u\r"))
	f.Add([]byte("ab\x1bbc\r"))
	f.Add([]byte("ab\x1b[1;3Dc\r"))
	f.Add([]byte("ab\x1b[1;3:1Dc\r"))
	f.Add([]byte("\x1b[3;5~"))

	f.Fuzz(func(t *testing.T, input []byte) {
		reader := newSurveyInput(fuzzFileReader{Reader: bytes.NewReader(input)})
		buf := make([]byte, 8)
		readLimit := len(input)*8 + 256
		readCount := 0

		for {
			n, err := reader.Read(buf)
			readCount += n
			if readCount > readLimit {
				t.Fatalf("read more bytes than expected: got %d, limit %d", readCount, readLimit)
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				t.Fatalf("unexpected read error: %v", err)
			}
		}
	})
}

func FuzzSurveyInputWithSurveyRuneReader(f *testing.F) {
	f.Add([]byte("See how this crashes:\x1bf\r"))
	f.Add([]byte("abc\x1b\x1b\r"))
	f.Add([]byte("abc\x1bq\r"))
	f.Add([]byte("\x1b[00\x1bX"))
	f.Add([]byte("abc\x17\r"))
	f.Add([]byte("abc\x1b\x7f\r"))
	f.Add([]byte("abc\x1b[3;3~\r"))
	f.Add([]byte("abc\x1b[127;3~\r"))
	f.Add([]byte("abc\x1b[8;3~\r"))
	f.Add([]byte("abc\x1b[127;3u\r"))
	f.Add([]byte("abc\x1b[8;3u\r"))
	f.Add([]byte("abc\x1b[3;3u\r"))
	f.Add([]byte("abc\x1b[27;3;127~\r"))
	f.Add([]byte("abc\x1b[27;3;100~\r"))
	f.Add([]byte("abc\x1b[100;3u\r"))
	f.Add([]byte("abc\x1b[57426;3u\r"))
	f.Add([]byte("ab\x1bbc\r"))
	f.Add([]byte("ab\x1b[1;3Dc\r"))
	f.Add([]byte("ab\x1b[1;3:1Dc\r"))
	f.Add([]byte("\x1b[3;5~"))

	f.Fuzz(func(t *testing.T, input []byte) {
		assertSurveyReaderAccepts(t, input)
	})
}

func assertSurveyReaderAccepts(t *testing.T, input []byte) {
	t.Helper()

	reader := newSurveyInput(fuzzFileReader{Reader: bytes.NewReader(input)})
	runeReader := terminal.NewRuneReader(terminal.Stdio{
		In:  reader,
		Out: fuzzFileWriter{Buffer: bytes.NewBuffer(nil)},
	})

	readLimit := len(input)*8 + 256
	for count := 0; count < readLimit; count++ {
		_, _, err := runeReader.ReadRune()
		if err == io.EOF {
			return
		}
		if err != nil {
			if strings.Contains(err.Error(), "unexpected escape sequence") {
				t.Fatalf("normalizer emitted a sequence Survey rejects: %v", err)
			}
			t.Fatalf("unexpected Survey read error: %v", err)
		}
	}

	t.Fatalf("Survey reader did not reach EOF within %d reads", readLimit)
}
