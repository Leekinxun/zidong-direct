package ssh

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var ErrTerminalInputEncoding = errors.New("当前终端编码无法表示这段输入，请切换到支持这些字符的编码")

func terminalEncoding(name string) (encoding.Encoding, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utf-8", "utf8":
		return nil, nil
	case "gbk":
		return simplifiedchinese.GBK, nil
	case "gb18030":
		return simplifiedchinese.GB18030, nil
	default:
		return nil, errors.New("不支持的终端编码")
	}
}

func decodeTerminalOutput(source io.Reader, charset encoding.Encoding) io.Reader {
	if charset == nil {
		return source
	}
	// Keep one decoder per session. SSH and WebSocket chunks do not
	// necessarily end at character boundaries.
	return transform.NewReader(source, charset.NewDecoder())
}

func encodeTerminalInput(input []byte, charset encoding.Encoding) ([]byte, error) {
	if charset == nil {
		return input, nil
	}
	// The API has already buffered a complete request body and the browser
	// queues complete UTF-8 strings. Fail the batch before sending anything
	// if a character cannot be represented in the selected charset.
	if !utf8.Valid(input) {
		return nil, ErrTerminalInputEncoding
	}
	encoded, err := charset.NewEncoder().Bytes(input)
	if err != nil {
		return nil, ErrTerminalInputEncoding
	}
	return encoded, nil
}
