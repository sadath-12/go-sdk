/*
Copyright 2021 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtime "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/go-sdk/service/common"
)

func TestTopicErrors(t *testing.T) {
	server := getTestServer()
	err := server.AddTopicEventHandler(nil, nil)
	require.Error(t, err, "expected error on nil sub with AddTopicEventHandler")

	err = server.AddBulkTopicEventHandler(nil, nil, 0, 0)
	require.Error(t, err, "expected error on nil sub with AddBulkTopicEventHandler")

	sub := &common.Subscription{}
	err = server.AddTopicEventHandler(sub, nil)
	require.Error(t, err, "expected error on invalid sub with AddTopicEventHandler")
	err = server.AddBulkTopicEventHandler(sub, nil, 0, 0)
	require.Error(t, err, "expected error on invalid sub with AddBulkTopicEventHandler")

	sub.PubsubName = "messages"
	err = server.AddTopicEventHandler(sub, nil)
	require.Error(t, err, "expected error on sub without topic with AddTopicEventHandler")
	sub.PubsubName = "messages"
	err = server.AddBulkTopicEventHandler(sub, nil, 0, 0)
	require.Error(t, err, "expected error on sub without topic with AddBulkTopicEventHandler")

	sub.Topic = "test"
	err = server.AddTopicEventHandler(sub, nil)
	require.Error(t, err, "expected error on sub without handler")
	err = server.AddBulkTopicEventHandler(sub, nil, 0, 0)
	require.Error(t, err, "expected error on sub without handler")
}

func TestTopicSubscriptionList(t *testing.T) {
	t.Run("With single event handling", func(t *testing.T) {
		server := getTestServer()

		sub1 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/test",
		}
		err := server.AddTopicEventHandler(sub1, eventHandler)
		require.NoError(t, err)
		resp, err := server.ListTopicSubscriptions(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.Len(t, resp.GetSubscriptions(), 1, "expected 1 handlers") {
			sub := resp.GetSubscriptions()[0]
			assert.Equal(t, "messages", sub.GetPubsubName())
			assert.Equal(t, "test", sub.Topic)
			assert.Nil(t, sub.GetRoutes())
		}

		// Add routing rule.
		sub2 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/other",
			Match:      `event.type == "other"`,
		}
		err = server.AddTopicEventHandler(sub2, eventHandler)
		require.NoError(t, err)
		resp, err = server.ListTopicSubscriptions(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.Len(t, resp.GetSubscriptions(), 1, "expected 1 handlers") {
			sub := resp.GetSubscriptions()[0]
			assert.Equal(t, "messages", sub.GetPubsubName())
			assert.Equal(t, "test", sub.Topic)
			if assert.NotNil(t, sub.GetRoutes()) {
				assert.Equal(t, "/test", sub.GetRoutes().Default)
				if assert.Len(t, sub.GetRoutes().Rules, 1) {
					rule := sub.GetRoutes().Rules[0]
					assert.Equal(t, "/other", rule.GetPath())
					assert.Equal(t, `event.type == "other"`, rule.GetMatch())
				}
			}
		}
	})
	t.Run("With bulk event handling", func(t *testing.T) {
		server := getTestServer()
		sub1 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/test",
		}
		err := server.AddBulkTopicEventHandler(sub1, eventHandler, 10, 1000)
		require.NoError(t, err)
		resp, err := server.ListTopicSubscriptions(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.Len(t, resp.GetSubscriptions(), 1, "expected 1 handlers") {
			sub := resp.GetSubscriptions()[0]
			assert.Equal(t, "messages", sub.GetPubsubName())
			assert.Equal(t, "test", sub.Topic)
			assert.Nil(t, sub.GetRoutes())
		}

		// Add routing rule.
		sub2 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/other",
			Match:      `event.type == "other"`,
		}
		err = server.AddBulkTopicEventHandler(sub2, eventHandler, 10, 1000)
		require.NoError(t, err)
		resp, err = server.ListTopicSubscriptions(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.Len(t, resp.GetSubscriptions(), 1, "expected 1 handlers") {
			sub := resp.GetSubscriptions()[0]
			assert.Equal(t, "messages", sub.GetPubsubName())
			assert.Equal(t, "test", sub.Topic)
			if assert.NotNil(t, sub.GetRoutes()) {
				assert.Equal(t, "/test", sub.GetRoutes().Default)
				if assert.Len(t, sub.GetRoutes().Rules, 1) {
					rule := sub.GetRoutes().Rules[0]
					assert.Equal(t, "/other", rule.GetPath())
					assert.Equal(t, `event.type == "other"`, rule.GetMatch())
				}
			}
		}
	})
}

// go test -timeout 30s ./service/grpc -count 1 -run ^TestTopic$
func TestTopic(t *testing.T) {
	t.Run("With single event handling", func(t *testing.T) {
		ctx := context.Background()

		sub := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
		}
		server := getTestServer()

		err := server.AddTopicEventHandler(sub, eventHandler)
		require.NoError(t, err)

		startTestServer(server)

		t.Run("topic event without request", func(t *testing.T) {
			_, err := server.OnTopicEvent(ctx, nil)
			require.Error(t, err)
		})

		t.Run("topic event for wrong topic", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Topic: "invalid",
			}
			_, err := server.OnTopicEvent(ctx, in)
			require.Error(t, err)
		})

		t.Run("topic event for valid topic", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub.Topic,
				PubsubName:      sub.PubsubName,
			}
			_, err := server.OnTopicEvent(ctx, in)
			require.NoError(t, err)
		})

		stopTestServer(t, server)
	})
	t.Run("With bulk event handling", func(t *testing.T) {
		ctx := context.Background()

		sub := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
		}
		server := getTestServer()

		err := server.AddBulkTopicEventHandler(sub, eventHandler, 10, 1000)
		require.NoError(t, err)

		startTestServer(server)

		t.Run("topic event without request", func(t *testing.T) {
			_, err := server.OnTopicEvent(ctx, nil)
			require.Error(t, err)
		})

		t.Run("topic event for wrong topic", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Topic: "invalid",
			}
			_, err := server.OnTopicEvent(ctx, in)
			require.Error(t, err)
		})

		t.Run("topic event for valid topic", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub.Topic,
				PubsubName:      sub.PubsubName,
			}
			_, err := server.OnTopicEvent(ctx, in)
			require.NoError(t, err)
		})

		stopTestServer(t, server)
	})
}

func TestTopicWithValidationDisabled(t *testing.T) {
	t.Run("With single event handling", func(t *testing.T) {
		ctx := context.Background()

		sub := &common.Subscription{
			PubsubName:             "messages",
			Topic:                  "*",
			DisableTopicValidation: true,
		}
		server := getTestServer()

		err := server.AddTopicEventHandler(sub, eventHandler)
		require.NoError(t, err)

		startTestServer(server)

		in := &runtime.TopicEventRequest{
			Id:              "a123",
			Source:          "test",
			Type:            "test",
			SpecVersion:     "v1.0",
			DataContentType: "text/plain",
			Data:            []byte("test"),
			Topic:           "test",
			PubsubName:      sub.PubsubName,
		}

		_, err = server.OnTopicEvent(ctx, in)
		require.NoError(t, err)
	})
	t.Run("With bulk event handling", func(t *testing.T) {
		ctx := context.Background()

		sub := &common.Subscription{
			PubsubName:             "messages",
			Topic:                  "*",
			DisableTopicValidation: true,
		}
		server := getTestServer()

		err := server.AddBulkTopicEventHandler(sub, eventHandler, 10, 1000)
		require.NoError(t, err)

		startTestServer(server)

		in := &runtime.TopicEventRequest{
			Id:              "a123",
			Source:          "test",
			Type:            "test",
			SpecVersion:     "v1.0",
			DataContentType: "text/plain",
			Data:            []byte("test"),
			Topic:           "test",
			PubsubName:      sub.PubsubName,
		}

		_, err = server.OnTopicEvent(ctx, in)
		require.NoError(t, err)
	})
}

func TestTopicWithErrors(t *testing.T) {
	t.Run("With single event handling", func(t *testing.T) {
		ctx := context.Background()

		sub1 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test1",
		}

		sub2 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test2",
		}
		server := getTestServer()

		err := server.AddTopicEventHandler(sub1, eventHandlerWithRetryError)
		require.NoError(t, err)

		err = server.AddTopicEventHandler(sub2, eventHandlerWithError)
		require.NoError(t, err)

		startTestServer(server)

		t.Run("topic event for retry error", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub1.Topic,
				PubsubName:      sub1.PubsubName,
			}
			resp, err := server.OnTopicEvent(ctx, in)
			require.Error(t, err)
			assert.Equal(t, resp.GetStatus(), runtime.TopicEventResponse_RETRY)
		})

		t.Run("topic event for error", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub2.Topic,
				PubsubName:      sub2.PubsubName,
			}
			resp, err := server.OnTopicEvent(ctx, in)
			require.NoError(t, err)
			assert.Equal(t, resp.GetStatus(), runtime.TopicEventResponse_DROP)
		})

		stopTestServer(t, server)
	})
	t.Run("With bulk event handling", func(t *testing.T) {
		ctx := context.Background()

		sub1 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test1",
		}

		sub2 := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test2",
		}
		server := getTestServer()

		err := server.AddBulkTopicEventHandler(sub1, eventHandlerWithRetryError, 10, 1000)
		require.NoError(t, err)

		err = server.AddBulkTopicEventHandler(sub2, eventHandlerWithError, 10, 1000)
		require.NoError(t, err)

		startTestServer(server)

		t.Run("topic event for retry error", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub1.Topic,
				PubsubName:      sub1.PubsubName,
			}
			resp, err := server.OnTopicEvent(ctx, in)
			require.Error(t, err)
			assert.Equal(t, resp.GetStatus(), runtime.TopicEventResponse_RETRY)
		})

		t.Run("topic event for error", func(t *testing.T) {
			in := &runtime.TopicEventRequest{
				Id:              "a123",
				Source:          "test",
				Type:            "test",
				SpecVersion:     "v1.0",
				DataContentType: "text/plain",
				Data:            []byte("test"),
				Topic:           sub2.Topic,
				PubsubName:      sub2.PubsubName,
			}
			resp, err := server.OnTopicEvent(ctx, in)
			require.NoError(t, err)
			assert.Equal(t, resp.GetStatus(), runtime.TopicEventResponse_DROP)
		})

		stopTestServer(t, server)
	})
}

func eventHandler(ctx context.Context, event *common.TopicEvent) (retry bool, err error) {
	if event == nil {
		return true, errors.New("nil event")
	}
	return false, nil
}

func eventHandlerWithRetryError(ctx context.Context, event *common.TopicEvent) (retry bool, err error) {
	return true, errors.New("nil event")
}

func eventHandlerWithError(ctx context.Context, event *common.TopicEvent) (retry bool, err error) {
	return false, errors.New("nil event")
}

func TestEventDataHandling(t *testing.T) {
	t.Run("With single event handling", func(t *testing.T) {
		ctx := context.Background()
		tests := map[string]struct {
			contentType string
			data        string
			value       interface{}
		}{
			"JSON bytes": {
				contentType: "application/json",
				data:        `{"message":"hello"}`,
				value: map[string]interface{}{
					"message": "hello",
				},
			},
			"JSON entension media type bytes": {
				contentType: "application/extension+json",
				data:        `{"message":"hello"}`,
				value: map[string]interface{}{
					"message": "hello",
				},
			},
			"Test": {
				contentType: "text/plain",
				data:        `message = hello`,
				value:       `message = hello`,
			},
			"Other": {
				contentType: "application/octet-stream",
				data:        `message = hello`,
				value:       []byte(`message = hello`),
			},
		}

		s := getTestServer()

		sub := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/test",
			Metadata:   map[string]string{},
		}

		recv := make(chan struct{}, 1)
		var topicEvent *common.TopicEvent
		handler := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
			topicEvent = e
			recv <- struct{}{}

			return false, nil
		}
		err := s.AddTopicEventHandler(sub, handler)
		require.NoError(t, err, "error adding event handler")

		startTestServer(s)

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				in := runtime.TopicEventRequest{
					Id:              "a123",
					Source:          "test",
					Type:            "test",
					SpecVersion:     "v1.0",
					DataContentType: tt.contentType,
					Data:            []byte(tt.data),
					Topic:           sub.Topic,
					PubsubName:      sub.PubsubName,
				}

				s.OnTopicEvent(ctx, &in)
				<-recv
				assert.Equal(t, tt.value, topicEvent.Data)
			})
		}
	})
	t.Run("With bulk event handling", func(t *testing.T) {
		ctx := context.Background()
		tests := map[string]struct {
			contentType string
			data        string
			value       interface{}
		}{
			"JSON bytes": {
				contentType: "application/json",
				data:        `{"message":"hello"}`,
				value: map[string]interface{}{
					"message": "hello",
				},
			},
			"JSON entension media type bytes": {
				contentType: "application/extension+json",
				data:        `{"message":"hello"}`,
				value: map[string]interface{}{
					"message": "hello",
				},
			},
			"Test": {
				contentType: "text/plain",
				data:        `message = hello`,
				value:       `message = hello`,
			},
			"Other": {
				contentType: "application/octet-stream",
				data:        `message = hello`,
				value:       []byte(`message = hello`),
			},
		}

		s := getTestServer()

		sub := &common.Subscription{
			PubsubName: "messages",
			Topic:      "test",
			Route:      "/test",
			Metadata:   map[string]string{},
		}

		recv := make(chan struct{}, 1)
		var topicEvent *common.TopicEvent
		handler := func(ctx context.Context, e *common.TopicEvent) (retry bool, err error) {
			topicEvent = e
			recv <- struct{}{}

			return false, nil
		}
		err := s.AddBulkTopicEventHandler(sub, handler, 10, 1000)
		require.NoError(t, err, "error adding event handler")

		startTestServer(s)

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				in := runtime.TopicEventRequest{
					Id:              "a123",
					Source:          "test",
					Type:            "test",
					SpecVersion:     "v1.0",
					DataContentType: tt.contentType,
					Data:            []byte(tt.data),
					Topic:           sub.Topic,
					PubsubName:      sub.PubsubName,
				}

				s.OnTopicEvent(ctx, &in)
				<-recv
				assert.Equal(t, tt.value, topicEvent.Data)
			})
		}
	})
}
