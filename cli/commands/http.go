package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
	"time"

	"github.com/BattlesnakeOfficial/rules/client"
)

type TimedHttpClient interface {
	Get(url string) (*http.Response, time.Duration, error)
	Post(url string, contentType string, body io.Reader) (*http.Response, time.Duration, error)
}

type timedHTTPClient struct {
	*http.Client
}

func (client timedHTTPClient) Get(url string) (*http.Response, time.Duration, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, 0, err
	}
	switch u.Scheme {
	case "http", "https", "":
		startTime := time.Now()
		res, err := client.Client.Get(url)
		return res, time.Since(startTime), err
	case "sim":
		return handleSimulatedRequest(u, http.MethodGet, nil)
	case "i2c":
		return handleSimulatedI2CRequest(u, http.MethodGet, nil)
	default:
		return nil, 0, fmt.Errorf("unsupported snake transport scheme %q", u.Scheme)
	}
}

func (client timedHTTPClient) Post(url string, contentType string, body io.Reader) (*http.Response, time.Duration, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, 0, err
	}
	switch u.Scheme {
	case "http", "https", "":
		startTime := time.Now()
		res, err := client.Client.Post(url, contentType, body)
		return res, time.Since(startTime), err
	case "sim":
		return handleSimulatedRequest(u, http.MethodPost, body)
	case "i2c":
		return handleSimulatedI2CRequest(u, http.MethodPost, body)
	default:
		return nil, 0, fmt.Errorf("unsupported snake transport scheme %q", u.Scheme)
	}
}

type simulatedPlayer struct {
	name      string
	author    string
	color     string
	head      string
	tail      string
	version   string
	moveMode  string
	latency   time.Duration
	i2cBus    string
	i2cAddr   string
	transport string
}

func handleSimulatedRequest(u *neturl.URL, method string, body io.Reader) (*http.Response, time.Duration, error) {
	startTime := time.Now()
	player, err := buildSimulatedPlayer(u, "sim")
	if err != nil {
		return nil, 0, err
	}

	payload, err := readBody(body)
	if err != nil {
		return nil, 0, err
	}

	res, err := player.handleHTTP(method, cleanEndpointPath(u), payload)
	return res, time.Since(startTime) + player.latency, err
}

func handleSimulatedI2CRequest(u *neturl.URL, method string, body io.Reader) (*http.Response, time.Duration, error) {
	startTime := time.Now()
	player, err := buildSimulatedPlayer(u, "i2c")
	if err != nil {
		return nil, 0, err
	}

	payload, err := readBody(body)
	if err != nil {
		return nil, 0, err
	}

	res, err := player.handleI2C(method, cleanEndpointPath(u), payload)
	return res, time.Since(startTime) + player.latency, err
}

func buildSimulatedPlayer(u *neturl.URL, transport string) (simulatedPlayer, error) {
	query := u.Query()
	moveMode := strings.ToLower(query.Get("move"))
	if moveMode == "" && transport != "i2c" {
		moveMode = strings.ToLower(strings.Trim(u.Host, "/"))
	}
	if moveMode == "" && transport != "i2c" {
		moveMode = strings.Trim(strings.Trim(u.Path, "/"), "/")
	}
	if moveMode == "" {
		moveMode = "up"
	}

	switch moveMode {
	case "up", "down", "left", "right", "clockwise", "counterclockwise":
	default:
		return simulatedPlayer{}, fmt.Errorf("unsupported simulated move mode %q", moveMode)
	}

	name := query.Get("name")
	if name == "" {
		name = "Sim " + asciiTitle(moveMode)
	}

	color := query.Get("color")
	if color == "" {
		color = "#2f8f6b"
	}

	latency := 5 * time.Millisecond
	if latencyMS := query.Get("latency_ms"); latencyMS != "" {
		if ms, err := time.ParseDuration(latencyMS + "ms"); err == nil {
			latency = ms
		}
	}

	player := simulatedPlayer{
		name:      name,
		author:    "BadgeSnake Sim",
		color:     color,
		head:      "pixel",
		tail:      "bolt",
		version:   "sim-0.1.0",
		moveMode:  moveMode,
		latency:   latency,
		i2cBus:    query.Get("bus"),
		i2cAddr:   query.Get("addr"),
		transport: transport,
	}

	if transport == "i2c" && player.i2cBus == "" {
		player.i2cBus = u.Host
	}
	if transport == "i2c" && player.i2cBus == "" {
		player.i2cBus = "stub"
	}
	if transport == "i2c" && player.i2cAddr == "" {
		player.i2cAddr = "0x10"
	}

	return player, nil
}

func cleanEndpointPath(u *neturl.URL) string {
	clean := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

func readBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	return ioutil.ReadAll(body)
}

func (player simulatedPlayer) handleHTTP(method string, endpoint string, payload []byte) (*http.Response, error) {
	switch {
	case method == http.MethodGet && endpoint == "/":
		return jsonResponse(http.StatusOK, client.SnakeMetadataResponse{
			APIVersion: "1",
			Author:     player.author,
			Color:      player.color,
			Head:       player.head,
			Tail:       player.tail,
			Version:    player.version,
		})
	case method == http.MethodPost && endpoint == "/start":
		return jsonResponse(http.StatusOK, map[string]string{})
	case method == http.MethodPost && endpoint == "/move":
		request := client.SnakeRequest{}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &request); err != nil {
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
		}
		return jsonResponse(http.StatusOK, client.MoveResponse{
			Move:  player.moveForTurn(request.Turn),
			Shout: player.transportShout(),
		})
	case method == http.MethodPost && endpoint == "/end":
		return jsonResponse(http.StatusOK, map[string]string{})
	default:
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "unsupported endpoint"})
	}
}

func (player simulatedPlayer) handleI2C(method string, endpoint string, payload []byte) (*http.Response, error) {
	token, err := tokenForRequest(method, endpoint)
	if err != nil {
		return nil, err
	}

	switch token {
	case tokenInfo:
		return jsonResponse(http.StatusOK, client.SnakeMetadataResponse{
			APIVersion: "1",
			Author:     player.author,
			Color:      player.color,
			Head:       player.head,
			Tail:       player.tail,
			Version:    player.version,
		})
	case tokenStart:
		return jsonResponse(http.StatusOK, map[string]string{"transport": "i2c"})
	case tokenMove:
		request := client.SnakeRequest{}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &request); err != nil {
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
		}
		return jsonResponse(http.StatusOK, client.MoveResponse{
			Move:  player.moveForTurn(request.Turn),
			Shout: player.transportShout(),
		})
	case tokenEnd:
		return jsonResponse(http.StatusOK, map[string]string{"transport": "i2c"})
	default:
		return jsonResponse(http.StatusNotFound, map[string]string{"error": "unsupported token"})
	}
}

func (player simulatedPlayer) moveForTurn(turn int) string {
	switch player.moveMode {
	case "up", "down", "left", "right":
		return player.moveMode
	case "clockwise":
		return []string{"up", "right", "down", "left"}[turn%4]
	case "counterclockwise":
		return []string{"up", "left", "down", "right"}[turn%4]
	default:
		return "up"
	}
}

func (player simulatedPlayer) transportShout() string {
	if player.transport == "i2c" {
		return fmt.Sprintf("i2c:%s@%s", player.i2cBus, player.i2cAddr)
	}
	return "simulated"
}

func jsonResponse(statusCode int, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		Header:     make(http.Header),
		Body:       ioutil.NopCloser(bytes.NewBuffer(body)),
		StatusCode: statusCode,
	}, nil
}

const (
	tokenInfo  = 0x01
	tokenStart = 0x02
	tokenMove  = 0x03
	tokenEnd   = 0x04
)

func tokenForRequest(method string, endpoint string) (byte, error) {
	switch {
	case method == http.MethodGet && endpoint == "/":
		return tokenInfo, nil
	case method == http.MethodPost && endpoint == "/start":
		return tokenStart, nil
	case method == http.MethodPost && endpoint == "/move":
		return tokenMove, nil
	case method == http.MethodPost && endpoint == "/end":
		return tokenEnd, nil
	default:
		return 0, fmt.Errorf("unsupported %s %s", method, endpoint)
	}
}

func asciiTitle(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
