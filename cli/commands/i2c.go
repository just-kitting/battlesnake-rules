package commands

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	i2cProtocolVersion   = 0x01
	i2cFrameHeaderSize   = 6
	defaultI2CReadLength = 512
)

const (
	i2cStatusSuccess     = 0x00
	i2cStatusBadRequest  = 0x01
	i2cStatusUnsupported = 0x02
	i2cStatusInternal    = 0x03
	i2cStatusBusy        = 0x04
)

type i2cFrame struct {
	Version byte
	Code    byte
	Flags   byte
	Payload []byte
}

type i2cTransferFunc func(args ...string) ([]byte, error)

var runI2CTransfer = defaultI2CTransfer

func handleLiveI2CRequest(u *neturl.URL, method string, body io.Reader) (*http.Response, error) {
	token, err := tokenForRequest(method, cleanEndpointPath(u))
	if err != nil {
		return nil, err
	}

	payload, err := readBody(body)
	if err != nil {
		return nil, err
	}

	spec, err := parseI2CSpec(u)
	if err != nil {
		return nil, err
	}

	reqFrame, err := encodeI2CFrame(i2cFrame{
		Version: i2cProtocolVersion,
		Code:    token,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}

	if _, err := runI2CTransfer(spec.writeArgs(reqFrame)...); err != nil {
		return nil, fmt.Errorf("i2c write failed on bus %s addr 0x%02x: %w", spec.Bus, spec.Address, err)
	}

	rawResponse, err := runI2CTransfer(spec.readArgs()...)
	if err != nil {
		return nil, fmt.Errorf("i2c read failed on bus %s addr 0x%02x: %w", spec.Bus, spec.Address, err)
	}

	respFrame, err := decodeI2CFramePrefix(parseI2CTransferOutput(rawResponse))
	if err != nil {
		return nil, fmt.Errorf("invalid i2c response frame from bus %s addr 0x%02x: %w", spec.Bus, spec.Address, err)
	}
	if respFrame.Version != i2cProtocolVersion {
		return nil, fmt.Errorf("unsupported i2c response version %d", respFrame.Version)
	}

	return i2cFrameToHTTPResponse(respFrame), nil
}

type i2cSpec struct {
	Bus           string
	Address       int
	ReadLength    int
	ForceTransfer bool
}

func parseI2CSpec(u *neturl.URL) (i2cSpec, error) {
	query := u.Query()
	bus := strings.TrimSpace(u.Host)
	if bus == "" {
		bus = strings.TrimSpace(query.Get("bus"))
	}
	if bus == "" {
		return i2cSpec{}, fmt.Errorf("missing i2c bus in URL host or bus query parameter")
	}
	bus = strings.TrimPrefix(bus, "i2c-")
	bus = strings.TrimPrefix(bus, "/dev/i2c-")

	addrText := query.Get("addr")
	if addrText == "" {
		return i2cSpec{}, fmt.Errorf("missing required i2c addr query parameter")
	}
	addr, err := strconv.ParseInt(addrText, 0, 16)
	if err != nil {
		return i2cSpec{}, fmt.Errorf("invalid i2c addr %q: %w", addrText, err)
	}
	if addr < 0 || addr > 0x7f {
		return i2cSpec{}, fmt.Errorf("i2c addr %q out of range", addrText)
	}

	readLength := defaultI2CReadLength
	if value := query.Get("max_response_len"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return i2cSpec{}, fmt.Errorf("invalid max_response_len %q: %w", value, err)
		}
		readLength = parsed
	}
	if readLength < i2cFrameHeaderSize {
		return i2cSpec{}, fmt.Errorf("max_response_len must be at least %d", i2cFrameHeaderSize)
	}

	return i2cSpec{
		Bus:           bus,
		Address:       int(addr),
		ReadLength:    readLength,
		ForceTransfer: true,
	}, nil
}

func (spec i2cSpec) writeArgs(payload []byte) []string {
	args := make([]string, 0, 4+len(payload))
	if spec.ForceTransfer {
		args = append(args, "-f")
	}
	args = append(args, "-y", spec.Bus, fmt.Sprintf("w%d@0x%02x", len(payload), spec.Address))
	for _, b := range payload {
		args = append(args, fmt.Sprintf("0x%02x", b))
	}
	return args
}

func (spec i2cSpec) readArgs() []string {
	args := make([]string, 0, 4)
	if spec.ForceTransfer {
		args = append(args, "-f")
	}
	args = append(args, "-y", spec.Bus, fmt.Sprintf("r%d@0x%02x", spec.ReadLength, spec.Address))
	return args
}

func defaultI2CTransfer(args ...string) ([]byte, error) {
	bin := os.Getenv("BADGESNAKE_I2CTRANSFER_BIN")
	if strings.TrimSpace(bin) == "" {
		bin = "i2ctransfer"
	}

	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", bin, args, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func encodeI2CFrame(frame i2cFrame) ([]byte, error) {
	if len(frame.Payload) > 0xffff {
		return nil, fmt.Errorf("payload too large: %d", len(frame.Payload))
	}

	buf := make([]byte, i2cFrameHeaderSize+len(frame.Payload))
	buf[0] = frame.Version
	buf[1] = frame.Code
	buf[2] = frame.Flags
	buf[3] = 0
	binary.LittleEndian.PutUint16(buf[4:6], uint16(len(frame.Payload)))
	copy(buf[i2cFrameHeaderSize:], frame.Payload)
	return buf, nil
}

func decodeI2CFramePrefix(buf []byte) (i2cFrame, error) {
	if len(buf) < i2cFrameHeaderSize {
		return i2cFrame{}, fmt.Errorf("frame too short: %d", len(buf))
	}

	payloadLen := int(binary.LittleEndian.Uint16(buf[4:6]))
	frameLen := i2cFrameHeaderSize + payloadLen
	if len(buf) < frameLen {
		return i2cFrame{}, fmt.Errorf("frame length mismatch: got=%d want-at-least=%d", len(buf), frameLen)
	}

	payload := make([]byte, payloadLen)
	copy(payload, buf[i2cFrameHeaderSize:frameLen])
	return i2cFrame{
		Version: buf[0],
		Code:    buf[1],
		Flags:   buf[2],
		Payload: payload,
	}, nil
}

func parseI2CTransferOutput(output []byte) []byte {
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return nil
	}

	buf := make([]byte, 0, len(fields))
	for _, field := range fields {
		cleaned := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(field)), "0x"), ",")
		if cleaned == "" {
			continue
		}
		value, err := strconv.ParseUint(cleaned, 16, 8)
		if err != nil {
			return nil
		}
		buf = append(buf, byte(value))
	}
	return buf
}

func i2cFrameToHTTPResponse(frame i2cFrame) *http.Response {
	body := frame.Payload
	if len(body) == 0 && frame.Code == i2cStatusSuccess {
		body = []byte(`{}`)
	}

	return &http.Response{
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		StatusCode: httpStatusFromI2CStatus(frame.Code),
	}
}

func httpStatusFromI2CStatus(status byte) int {
	switch status {
	case i2cStatusSuccess:
		return http.StatusOK
	case i2cStatusBadRequest:
		return http.StatusBadRequest
	case i2cStatusUnsupported:
		return http.StatusNotFound
	case i2cStatusBusy:
		return http.StatusServiceUnavailable
	case i2cStatusInternal:
		fallthrough
	default:
		return http.StatusInternalServerError
	}
}
