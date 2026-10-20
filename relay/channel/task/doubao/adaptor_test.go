package doubao

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedanceBillingPricesByResolutionAndReferenceVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		model       string
		resolution  string
		reference   string
		basePrice   float64
		actualPrice float64
	}{
		{"doubao-seedance-2-0-mini-260615", "480p", "", 23, 23},
		{"doubao-seedance-2-0-mini-260615", "480p", "video_url", 23, 14},
		{"doubao-seedance-2-0-mini-260615", "720p", "", 23, 23},
		{"doubao-seedance-2-0-mini-260615", "720p", "video_url", 23, 14},
		{"doubao-seedance-2-0-mini-260615", "720p", "image_url", 23, 23},
		{"doubao-seedance-2-5-260628", "480p", "", 70, 70},
		{"doubao-seedance-2-5-260628", "480p", "video_url", 70, 42},
		{"doubao-seedance-2-5-260628", "720p", "", 70, 70},
		{"doubao-seedance-2-5-260628", "720p", "video_url", 70, 42},
		{"doubao-seedance-2-5-260628", "1080p", "", 70, 77},
		{"doubao-seedance-2-5-260628", " 1080P ", "video_url", 70, 46},
		{"doubao-seedance-2-0-260128", "4k", "video_url", 46, 16},
		{"doubao-seedance-2-0-fast-260128", "720p", "video_url", 37, 22},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s/%s", tc.model, tc.resolution, tc.reference), func(t *testing.T) {
			metadata := map[string]any{"resolution": tc.resolution, "duration": 6}
			if tc.reference != "" {
				metadata["content"] = []any{map[string]any{
					"type":       tc.reference,
					tc.reference: map[string]any{"url": "https://example.com/reference"},
				}}
			}
			payload, err := common.Marshal(map[string]any{
				"model": tc.model, "prompt": "A paper boat at sea", "metadata": metadata,
			})
			require.NoError(t, err)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader(payload))
			ctx.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

			adaptor := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{
				OriginModelName: tc.model,
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
			ratios := adaptor.EstimateBilling(ctx, info)
			if tc.actualPrice == tc.basePrice {
				assert.Empty(t, ratios)
				return
			}
			require.Len(t, ratios, 1)
			assert.InDelta(t, tc.actualPrice/tc.basePrice, ratios["video_input"], 1e-12)
		})
	}
}

func TestSeedanceOfficialBuildRequestBodyPreservesNativeContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(common.KeySeedanceOfficialAPI, true)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/seedance/api/v3/contents/generations/tasks", bytes.NewBufferString(`{
		"model":"doubao-seedance-1-5-pro",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/a.png"},"role":"first_frame"},
			{"type":"text","text":"make a video"}
		],
		"duration":5,
		"watermark":false
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "upstream-seedance-model"},
	}

	body, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)

	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(raw, &payload))
	assert.Equal(t, "upstream-seedance-model", payload["model"])
	assert.Equal(t, float64(5), payload["duration"])
	assert.Equal(t, false, payload["watermark"])

	content, ok := payload["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 2)
	first, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "image_url", first["type"])
	assert.Equal(t, "first_frame", first["role"])
}

func TestSeedanceOfficialDoResponseReturnsUpstreamTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(common.KeySeedanceOfficialAPI, true)

	adaptor := &TaskAdaptor{}
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewBufferString(`{"id":"cgt-upstream"}`)),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	taskID, taskData, taskErr := adaptor.DoResponse(ctx, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "cgt-upstream", taskID)
	assert.JSONEq(t, `{"id":"cgt-upstream"}`, string(taskData))
	assert.JSONEq(t, `{"id":"cgt-upstream"}`, recorder.Body.String())
}
