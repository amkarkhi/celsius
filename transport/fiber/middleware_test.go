package fiber_test

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amkarkhi/celsius"
	celsiusfiber "github.com/amkarkhi/celsius/transport/fiber"
	celcel "github.com/google/cel-go/cel"
)

type Out struct {
	Message string `mapstructure:"message"`
}

type Input struct{ UID int }

func (i *Input) Map() map[string]any { return map[string]any{"uid": i.UID} }
func (i *Input) Parse(c celsius.Ctx) (map[string]any, error) {
	v, _ := strconv.Atoi(c.Header("X-UID", "-1"))
	i.UID = v
	return i.Map(), nil
}

const rules = `
rules:
  greeting:
    - script: 'uid > 100'
      tag:    "vip"
      out: {message: "vip"}
    - script: 'true'
      tag:    "default"
      out: {message: "default"}
variables: {}
`

func setup(t *testing.T) *celsius.Engine[Out] {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "rules.yaml")
	require.NoError(t, os.WriteFile(p, []byte(rules), 0o644))
	e, err := celsius.New[Out](celsius.Config{
		Source: celsius.FileSource(p),
		Env:    celsius.DefaultEnv().With(celsius.Variable("uid", celcel.IntType)),
		Input:  &Input{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e
}

func TestFiberMiddlewareAttachesRule(t *testing.T) {
	e := setup(t)

	app := fiber.New()
	app.Use(celsiusfiber.Middleware(e, "greeting"))
	app.Get("/", func(c *fiber.Ctx) error {
		rule, ok := celsius.ResultFrom[Out](c.UserContext())
		if !ok {
			return c.SendString("none")
		}
		return c.SendString(rule.Out.Message)
	})

	cases := []struct {
		uid  string
		want string
	}{
		{"200", "vip"},
		{"5", "default"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-UID", tc.uid)
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, tc.want, string(body))
	}
}

func TestFiberMiddlewareNoMatch(t *testing.T) {
	e := setup(t)

	app := fiber.New()
	app.Use(celsiusfiber.Middleware(e, "missing-group"))
	app.Get("/", func(c *fiber.Ctx) error {
		_, ok := celsius.ResultFrom[Out](c.UserContext())
		if ok {
			return c.SendString("unexpected")
		}
		return c.SendString("none")
	})

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "none", string(body))
}
