package gin_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amkarkhi/celsius"
	celsiusgin "github.com/amkarkhi/celsius/transport/gin"
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

func TestGinMiddlewareAttachesRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := setup(t)

	r := gin.New()
	r.Use(celsiusgin.Middleware(e, "greeting"))
	r.GET("/", func(c *gin.Context) {
		rule, ok := celsius.ResultFrom[Out](c.Request.Context())
		if !ok {
			c.String(http.StatusOK, "none")
			return
		}
		c.String(http.StatusOK, rule.Out.Message)
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
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, tc.want, w.Body.String())
	}
}

func TestGinMiddlewareNoMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := setup(t)

	r := gin.New()
	r.Use(celsiusgin.Middleware(e, "missing-group"))
	r.GET("/", func(c *gin.Context) {
		_, ok := celsius.ResultFrom[Out](c.Request.Context())
		if ok {
			c.String(http.StatusOK, "unexpected")
			return
		}
		c.String(http.StatusOK, "none")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-UID", "1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, "none", w.Body.String())
}
