package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/BattlesnakeOfficial/rules/client"
	"github.com/stretchr/testify/require"
)

func TestTimedHTTPClientGetSimulatedMetadata(t *testing.T) {
	c := timedHTTPClient{&http.Client{}}

	res, latency, err := c.Get("sim://clockwise?name=Clocky&color=%23112233")
	require.NoError(t, err)
	require.Greater(t, latency.Milliseconds(), int64(0))
	require.Equal(t, http.StatusOK, res.StatusCode)

	defer res.Body.Close()
	var metadata client.SnakeMetadataResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&metadata))
	require.Equal(t, "BadgeSnake Sim", metadata.Author)
	require.Equal(t, "#112233", metadata.Color)
	require.Equal(t, "pixel", metadata.Head)
}

func TestTimedHTTPClientPostSimulatedMove(t *testing.T) {
	c := timedHTTPClient{&http.Client{}}
	req := client.SnakeRequest{
		Game: client.Game{ID: "game-1", Timeout: 500},
		Turn: 3,
		Board: client.Board{
			Height: 11,
			Width:  11,
			Snakes: []client.Snake{},
			Food:   []client.Coord{},
		},
		You: client.Snake{ID: "player-1"},
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	res, _, err := c.Post("sim://clockwise/move", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	defer res.Body.Close()
	var move client.MoveResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&move))
	require.Equal(t, "left", move.Move)
	require.Equal(t, "simulated", move.Shout)
}

func TestTimedHTTPClientPostI2CMove(t *testing.T) {
	c := timedHTTPClient{&http.Client{}}
	req := client.SnakeRequest{
		Game: client.Game{ID: "game-1", Timeout: 500},
		Turn: 1,
		Board: client.Board{
			Height: 11,
			Width:  11,
			Snakes: []client.Snake{},
			Food:   []client.Coord{},
		},
		You: client.Snake{ID: "player-1"},
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	res, _, err := c.Post("i2c://stub/move?addr=0x10&move=counterclockwise", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	defer res.Body.Close()
	var move client.MoveResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&move))
	require.Equal(t, "left", move.Move)
	require.Equal(t, "i2c:stub@0x10", move.Shout)
}

func TestTimedHTTPClientPostLiveI2CMove(t *testing.T) {
	originalRunner := runI2CTransfer
	defer func() {
		runI2CTransfer = originalRunner
	}()

	var calls [][]string
	runI2CTransfer = func(args ...string) ([]byte, error) {
		copied := append([]string(nil), args...)
		calls = append(calls, copied)

		if len(calls) == 1 {
			require.Equal(t, []string{"-f", "-y", "1"}, copied[:3])
			require.True(t, strings.HasPrefix(copied[3], "w"), "expected write transfer descriptor")
			require.Contains(t, copied[3], "@0x42")
			return nil, nil
		}

		require.Equal(t, []string{"-f", "-y", "1", "r32@0x42"}, copied)
		return encodeI2CTestFrame(t, i2cStatusSuccess, []byte(`{"move":"right","shout":"zepto"}`), 32), nil
	}

	c := timedHTTPClient{&http.Client{}}
	req := client.SnakeRequest{
		Game: client.Game{ID: "game-1", Timeout: 500},
		Turn: 2,
		Board: client.Board{
			Height: 11,
			Width:  11,
			Snakes: []client.Snake{},
			Food:   []client.Coord{},
		},
		You: client.Snake{ID: "player-1"},
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	res, _, err := c.Post("i2c://1/move?addr=0x42&max_response_len=32", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Len(t, calls, 2)

	defer res.Body.Close()
	var move client.MoveResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&move))
	require.Equal(t, "right", move.Move)
	require.Equal(t, "zepto", move.Shout)
}

func TestTimedHTTPClientGetLiveI2CMetadata(t *testing.T) {
	originalRunner := runI2CTransfer
	defer func() {
		runI2CTransfer = originalRunner
	}()

	runI2CTransfer = func(args ...string) ([]byte, error) {
		if strings.HasPrefix(args[3], "w") {
			return nil, nil
		}
		return encodeI2CTestFrame(t, i2cStatusSuccess, []byte(`{"apiversion":"1","author":"Zepto","color":"#123456","head":"pixel","tail":"bolt","version":"0.1.0"}`), defaultI2CReadLength), nil
	}

	c := timedHTTPClient{&http.Client{}}
	res, _, err := c.Get("i2c://i2c-3?addr=0x21")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	defer res.Body.Close()
	var metadata client.SnakeMetadataResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&metadata))
	require.Equal(t, "Zepto", metadata.Author)
	require.Equal(t, "#123456", metadata.Color)
	require.Equal(t, "pixel", metadata.Head)
}

func TestBuildSnakesFromOptionsWithSimulatedURL(t *testing.T) {
	gameState := buildDefaultGameState()
	gameState.Names = []string{"Righty"}
	gameState.URLs = []string{"sim://right?name=Righty&color=%2300ff00"}

	require.NoError(t, gameState.Initialize())

	snakes, err := gameState.buildSnakesFromOptions()
	require.NoError(t, err)
	require.Len(t, snakes, 1)

	for _, snake := range snakes {
		require.Equal(t, "Righty", snake.Name)
		require.Equal(t, "sim://right?name=Righty&color=%2300ff00", snake.URL)
		require.Equal(t, "#00ff00", snake.Color)
		require.Equal(t, "pixel", snake.Head)
	}
}

func encodeI2CTestFrame(t *testing.T, status byte, payload []byte, paddedLen int) []byte {
	t.Helper()

	frame, err := encodeI2CFrame(i2cFrame{
		Version: i2cProtocolVersion,
		Code:    status,
		Payload: payload,
	})
	require.NoError(t, err)

	require.LessOrEqual(t, len(frame), paddedLen)
	padded := make([]byte, paddedLen)
	copy(padded, frame)

	parts := make([]string, 0, len(padded))
	for _, b := range padded {
		parts = append(parts, "0x"+strings.ToUpper(hexByte(b)))
	}
	return []byte(strings.Join(parts, " "))
}

func hexByte(value byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[value>>4], digits[value&0x0F]})
}
