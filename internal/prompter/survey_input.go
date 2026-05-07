package prompter

import (
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	ghPrompter "github.com/cli/go-gh/v2/pkg/prompter"
)

type surveyInput struct {
	input   ghPrompter.FileReader
	pending []byte
	line    []rune
	cursor  int
}

func newSurveyInput(input ghPrompter.FileReader) ghPrompter.FileReader {
	return &surveyInput{input: input}
}

func (i *surveyInput) Fd() uintptr {
	return i.input.Fd()
}

func (i *surveyInput) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if len(i.pending) > 0 {
		n := copy(p, i.pending)
		i.pending = i.pending[n:]
		return n, nil
	}

	b, err := i.readByte()
	if err != nil {
		return 0, err
	}

	if b == '\x17' {
		replacement := i.deletePreviousWordReplacement()
		n := copy(p, replacement)
		i.pending = append(i.pending, replacement[n:]...)
		return n, nil
	}

	if b != '\x1b' {
		i.recordRune(b)
		p[0] = b
		return 1, nil
	}

	replacement, err := i.readEscape()
	if err != nil {
		p[0] = b
		if err == io.EOF {
			return 1, nil
		}
		return 1, err
	}

	n := copy(p, replacement)
	i.pending = append(i.pending, replacement[n:]...)
	return n, nil
}

func (i *surveyInput) readEscape() ([]byte, error) {
	next, err := i.readByte()
	if err != nil {
		return nil, err
	}

	switch next {
	case '\x1b':
		return []byte{'\x1b'}, nil
	case '\x7f', '\b':
		return i.deletePreviousWordReplacement(), nil
	case 'f', 'F':
		count := i.moveForwardWord()
		if count == 0 {
			return []byte{'\x00'}, nil
		}
		return []byte(strings.Repeat("\x1b[C", count)), nil
	case 'b', 'B':
		count := i.moveBackwardWord()
		if count == 0 {
			return []byte{'\x00'}, nil
		}
		return []byte(strings.Repeat("\x1b[D", count)), nil
	case 'a', 'A':
		i.cursor = 0
		return []byte("\x1b[H"), nil
	case 'e', 'E':
		i.cursor = len(i.line)
		return []byte("\x1b[F"), nil
	case 'd', 'D':
		return i.deletePreviousWordReplacement(), nil
	case '[', 'O':
		return i.readControlSequence(next)
	default:
		i.recordRune(next)
		return []byte{next}, nil
	}
}

func (i *surveyInput) readControlSequence(keypad byte) ([]byte, error) {
	sequence := []byte{'\x1b', keypad}
	for {
		b, err := i.readByte()
		if err != nil {
			return sequence, err
		}
		if b == '\x1b' {
			replacement, err := i.readEscape()
			if err != nil {
				if err == io.EOF {
					return []byte{'\x00', '\x1b'}, nil
				}
				return []byte{'\x00'}, err
			}
			return append([]byte{'\x00'}, replacement...), nil
		}
		sequence = append(sequence, b)
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' {
			break
		}
	}

	final := sequence[len(sequence)-1]
	params, hasParams := parseCSIParams(sequence)
	switch final {
	case 'C':
		if hasParams && hasAltModifier(csiParam(params, 1)) {
			count := i.moveForwardWord()
			if count == 0 {
				return []byte{'\x00'}, nil
			}
			return []byte(strings.Repeat("\x1b[C", count)), nil
		}
		if i.cursor < len(i.line) {
			i.cursor++
		}
		return []byte{'\x1b', keypad, final}, nil
	case 'D':
		if hasParams && hasAltModifier(csiParam(params, 1)) {
			count := i.moveBackwardWord()
			if count == 0 {
				return []byte{'\x00'}, nil
			}
			return []byte(strings.Repeat("\x1b[D", count)), nil
		}
		if i.cursor > 0 {
			i.cursor--
		}
		return []byte{'\x1b', keypad, final}, nil
	case 'F':
		i.cursor = len(i.line)
		return []byte{'\x1b', keypad, final}, nil
	case 'H':
		i.cursor = 0
		return []byte{'\x1b', keypad, final}, nil
	case 'A', 'B':
		return []byte{'\x1b', keypad, final}, nil
	case '~':
		if hasParams {
			key := csiParam(params, 0)
			modifier := csiParam(params, 1)
			if key == 3 {
				if hasAltModifier(modifier) {
					return i.deletePreviousWordReplacement(), nil
				}
				i.deleteAfterCursor()
				return []byte("\x1b[3~"), nil
			}
			if hasAltModifier(modifier) && (key == 127 || key == 8) {
				return i.deletePreviousWordReplacement(), nil
			}
			if key == 27 && hasAltModifier(modifier) {
				if replacement, ok := i.replacementForMetaKey(csiParam(params, 2)); ok {
					return replacement, nil
				}
			}
		}
	case 'u':
		if hasParams && hasAltModifier(csiParam(params, 1)) {
			key := csiParam(params, 0)
			if key == 57426 {
				return i.deletePreviousWordReplacement(), nil
			}
			if replacement, ok := i.replacementForMetaKey(key); ok {
				return replacement, nil
			}
		}
	}
	return sequence, nil
}

func (i *surveyInput) replacementForMetaKey(key int) ([]byte, bool) {
	switch key {
	case 127, 8, 3, 'd', 'D':
		return i.deletePreviousWordReplacement(), true
	case 'f', 'F':
		count := i.moveForwardWord()
		if count == 0 {
			return []byte{'\x00'}, true
		}
		return []byte(strings.Repeat("\x1b[C", count)), true
	case 'b', 'B':
		count := i.moveBackwardWord()
		if count == 0 {
			return []byte{'\x00'}, true
		}
		return []byte(strings.Repeat("\x1b[D", count)), true
	case 'a', 'A':
		i.cursor = 0
		return []byte("\x1b[H"), true
	case 'e', 'E':
		i.cursor = len(i.line)
		return []byte("\x1b[F"), true
	default:
		return nil, false
	}
}

func (i *surveyInput) deletePreviousWordReplacement() []byte {
	count := i.deletePreviousWord()
	if count == 0 {
		return []byte{'\x00'}
	}
	return []byte(strings.Repeat("\x7f", count))
}

func parseCSIParams(sequence []byte) ([]int, bool) {
	if len(sequence) < 4 || sequence[0] != '\x1b' || sequence[1] != '[' {
		return nil, false
	}

	payload := string(sequence[2 : len(sequence)-1])
	parts := strings.Split(payload, ";")
	if len(parts) == 0 {
		return nil, false
	}

	params := make([]int, 0, len(parts))
	for _, part := range parts {
		valuePart, _, _ := strings.Cut(part, ":")
		value, err := strconv.Atoi(valuePart)
		if err != nil {
			return nil, false
		}
		params = append(params, value)
	}

	return params, true
}

func csiParam(params []int, index int) int {
	if index < 0 || index >= len(params) {
		return 0
	}
	return params[index]
}

func hasAltModifier(modifier int) bool {
	if modifier <= 1 {
		return false
	}
	return ((modifier - 1) & 2) != 0
}

func (i *surveyInput) readByte() (byte, error) {
	var b [1]byte
	for {
		n, err := i.input.Read(b[:])
		if n > 0 {
			return b[0], nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func (i *surveyInput) recordRune(b byte) {
	switch b {
	case '\x7f', '\b':
		i.deleteBeforeCursor()
	case '\r', '\n':
	case '\x00':
	default:
		if b < ' ' {
			return
		}
		i.line = slices.Insert(i.line, i.cursor, rune(b))
		i.cursor++
	}
}

func (i *surveyInput) deleteBeforeCursor() bool {
	if i.cursor == 0 {
		return false
	}
	i.line = slices.Delete(i.line, i.cursor-1, i.cursor)
	i.cursor--
	return true
}

func (i *surveyInput) deleteAfterCursor() bool {
	if i.cursor >= len(i.line) {
		return false
	}
	i.line = slices.Delete(i.line, i.cursor, i.cursor+1)
	return true
}

func (i *surveyInput) deletePreviousWord() int {
	start := i.cursor
	for i.cursor > 0 && unicode.IsSpace(i.line[i.cursor-1]) {
		i.deleteBeforeCursor()
	}
	for i.cursor > 0 && !unicode.IsSpace(i.line[i.cursor-1]) {
		i.deleteBeforeCursor()
	}
	return start - i.cursor
}

func (i *surveyInput) moveBackwardWord() int {
	start := i.cursor
	for i.cursor > 0 && unicode.IsSpace(i.line[i.cursor-1]) {
		i.cursor--
	}
	for i.cursor > 0 && !unicode.IsSpace(i.line[i.cursor-1]) {
		i.cursor--
	}
	return start - i.cursor
}

func (i *surveyInput) moveForwardWord() int {
	start := i.cursor
	for i.cursor < len(i.line) && unicode.IsSpace(i.line[i.cursor]) {
		i.cursor++
	}
	for i.cursor < len(i.line) && !unicode.IsSpace(i.line[i.cursor]) {
		i.cursor++
	}
	return i.cursor - start
}
