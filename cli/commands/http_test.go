package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
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
