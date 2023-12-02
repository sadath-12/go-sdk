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

package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	actorErr "github.com/dapr/go-sdk/actor/error"
	"github.com/dapr/go-sdk/actor/runtime"
	"github.com/dapr/go-sdk/service/common"
	"github.com/dapr/go-sdk/service/internal"
)

const (
	// PubSubHandlerSuccessStatusCode is the successful ack code for pubsub event appcallback response.
	PubSubHandlerSuccessStatusCode int = http.StatusOK

	// PubSubHandlerRetryStatusCode is the error response code (nack) pubsub event appcallback response.
	PubSubHandlerRetryStatusCode int = http.StatusInternalServerError

	// PubSubHandlerDropStatusCode is the pubsub event appcallback response code indicating that Dapr should drop that message.
	PubSubHandlerDropStatusCode int = http.StatusSeeOther
)

// topicEventJSON is identical to `common.TopicEvent`
// except for it treats `data` as a json.RawMessage so it can
// be used as bytes or interface{}.
// Merge itemMap into topicEventJSON
type topicEventJSON struct {
	// ID identifies the event.
	ID string `json:"id"` // y
	// The version of the CloudEvents specification.
	SpecVersion string `json:"specversion"`
	// The type of event related to the originating occurrence.
	Type string `json:"type"`
	// Source identifies the context in which an event happened.
	Source string `json:"source"`
	// The content type of data value.
	DataContentType string `json:"datacontenttype"`
	// The content of the event.
	// Note, this is why the gRPC and HTTP implementations need separate structs for cloud events.
	Data json.RawMessage `json:"data"`
	// The base64 encoding content of the event.
	// Note, this is processing rawPayload and binary content types.
	DataBase64 string `json:"data_base64,omitempty"`
	// Cloud event subject
	Subject string `json:"subject"`
	// The pubsub topic which publisher sent to.
	Topic string `json:"topic"`
	// PubsubName is name of the pub/sub this message came from
	PubsubName string `json:"pubsubname"`
}

type status string

const (
	// SubscriptionResponseStatusSuccess indicates that the subscription event was processed successfully.
	SubscriptionResponseStatusSuccess status = "SUCCESS"
)

type BulkSubscribeResponseEntry struct {
	// The id of the bulk subscribe entry
	entryId string

	// The response status of the bulk subscribe entry
	status status
}

type BulkSubscribeResponse struct {
	statuses []BulkSubscribeResponseEntry
}

func (s *Server) registerBaseHandler() {
	// register subscribe handler
	f := func(w http.ResponseWriter, r *http.Request) {
		subs := make([]*internal.TopicSubscription, 0, len(s.topicRegistrar))
		for _, s := range s.topicRegistrar {
			subs = append(subs, s.Subscription)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(subs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.mux.HandleFunc("/dapr/subscribe", f)

	// register health check handler
	fHealth := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
	s.mux.Get("/healthz", fHealth)

	// register actor config handler
	fRegister := func(w http.ResponseWriter, r *http.Request) {
		data, err := runtime.GetActorRuntimeInstanceContext().GetJSONSerializedConfig()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err = w.Write(data); err != nil {
			return
		}
	}
	s.mux.Get("/dapr/config", fRegister)

	// register actor method invoke handler
	fInvoke := func(w http.ResponseWriter, r *http.Request) {
		actorType := chi.URLParam(r, "actorType")
		actorID := chi.URLParam(r, "actorId")
		methodName := chi.URLParam(r, "methodName")
		reqData, _ := io.ReadAll(r.Body)
		rspData, err := runtime.GetActorRuntimeInstanceContext().InvokeActorMethod(r.Context(), actorType, actorID, methodName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rspData)
	}
	s.mux.Put("/actors/{actorType}/{actorId}/method/{methodName}", fInvoke)

	// register deactivate actor handler
	fDelete := func(w http.ResponseWriter, r *http.Request) {
		actorType := chi.URLParam(r, "actorType")
		actorID := chi.URLParam(r, "actorId")
		err := runtime.GetActorRuntimeInstanceContext().Deactivate(r.Context(), actorType, actorID)
		if err == actorErr.ErrActorTypeNotFound || err == actorErr.ErrActorIDNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.Delete("/actors/{actorType}/{actorId}", fDelete)

	// register actor reminder invoke handler
	fReminder := func(w http.ResponseWriter, r *http.Request) {
		actorType := chi.URLParam(r, "actorType")
		actorID := chi.URLParam(r, "actorId")
		reminderName := chi.URLParam(r, "reminderName")
		reqData, _ := io.ReadAll(r.Body)
		err := runtime.GetActorRuntimeInstanceContext().InvokeReminder(r.Context(), actorType, actorID, reminderName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.Put("/actors/{actorType}/{actorId}/method/remind/{reminderName}", fReminder)

	// register actor timer invoke handler
	fTimer := func(w http.ResponseWriter, r *http.Request) {
		actorType := chi.URLParam(r, "actorType")
		actorID := chi.URLParam(r, "actorId")
		timerName := chi.URLParam(r, "timerName")
		reqData, _ := io.ReadAll(r.Body)
		err := runtime.GetActorRuntimeInstanceContext().InvokeTimer(r.Context(), actorType, actorID, timerName, reqData)
		if err == actorErr.ErrActorTypeNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		if err != actorErr.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
	}
	s.mux.Put("/actors/{actorType}/{actorId}/method/timer/{timerName}", fTimer)
}

// AddTopicEventHandler appends provided event handler with it's name to the service.
func (s *Server) AddTopicEventHandler(sub *common.Subscription, fn common.TopicEventHandler) error {
	if sub == nil {
		return errors.New("subscription required")
	}
	// Route is only required for HTTP but should be specified for the
	// app protocol to be interchangeable.
	if sub.Route == "" {
		return errors.New("handler route name")
	}
	if err := s.topicRegistrar.AddSubscription(sub, fn); err != nil {
		return err
	}

	s.mux.Handle(sub.Route, optionsHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// check for post with no data
			var (
				body []byte
				err  error
			)
			if r.Body != nil {
				body, err = io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
					return
				}
			}
			if len(body) == 0 {
				http.Error(w, "nil content", PubSubHandlerDropStatusCode)
				return
			}

			// deserialize the event
			var in topicEventJSON
			if err = json.Unmarshal(body, &in); err != nil {
				http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
				return
			}

			if in.PubsubName == "" {
				in.Topic = sub.PubsubName
			}
			if in.Topic == "" {
				in.Topic = sub.Topic
			}

			data, rawData := in.getData()
			te := common.TopicEvent{
				ID:              in.ID,
				SpecVersion:     in.SpecVersion,
				Type:            in.Type,
				Source:          in.Source,
				DataContentType: in.DataContentType,
				Data:            data,
				RawData:         rawData,
				DataBase64:      in.DataBase64,
				Subject:         in.Subject,
				PubsubName:      in.PubsubName,
				Topic:           in.Topic,
			}

			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			// execute user handler
			retry, err := fn(r.Context(), &te)
			if err == nil {
				writeStatus(w, common.SubscriptionResponseStatusSuccess)
				return
			}

			if retry {
				writeStatus(w, common.SubscriptionResponseStatusRetry)
				return
			}

			writeStatus(w, common.SubscriptionResponseStatusDrop)
		})))

	return nil
}

func (in topicEventJSON) getData() (data any, rawData []byte) {
	var (
		err error
		v   any
	)
	if len(in.Data) > 0 {
		rawData = []byte(in.Data)
		data = rawData
		// We can assume that rawData is valid JSON
		// without checking in.DataContentType == "application/json".
		if err = json.Unmarshal(rawData, &v); err == nil {
			data = v
			// Handling of JSON base64 encoded or escaped in a string.
			if str, ok := v.(string); ok {
				// This is the path that will most likely succeed.
				var (
					vString any
					decoded []byte
				)
				if err = json.Unmarshal([]byte(str), &vString); err == nil {
					data = vString
				} else if decoded, err = base64.StdEncoding.DecodeString(str); err == nil {
					// Decoded Base64 encoded JSON does not seem to be in the spec
					// but it is in existing unit tests so this handles that case.
					var vBase64 any
					if err = json.Unmarshal(decoded, &vBase64); err == nil {
						data = vBase64
					}
				}
			}
		}
	} else if in.DataBase64 != "" {
		rawData, err = base64.StdEncoding.DecodeString(in.DataBase64)
		if err == nil {
			data = rawData
			if in.DataContentType == "application/json" {
				if err = json.Unmarshal(rawData, &v); err == nil {
					data = v
				}
			}
		}
	}

	return data, rawData
}

func (in BulkTopicJson) getData() (data any, rawData []byte) {
	var (
		err error
		v   any
	)
	if len(in.Data) > 0 {
		rawData = []byte(in.Data)
		data = rawData
		// We can assume that rawData is valid JSON
		// without checking in.DataContentType == "application/json".
		if err = json.Unmarshal(rawData, &v); err == nil {
			data = v
			// Handling of JSON base64 encoded or escaped in a string.
			if str, ok := v.(string); ok {
				// This is the path that will most likely succeed.
				var (
					vString any
					decoded []byte
				)
				if err = json.Unmarshal([]byte(str), &vString); err == nil {
					data = vString
				} else if decoded, err = base64.StdEncoding.DecodeString(str); err == nil {
					// Decoded Base64 encoded JSON does not seem to be in the spec
					// but it is in existing unit tests so this handles that case.
					var vBase64 any
					if err = json.Unmarshal(decoded, &vBase64); err == nil {
						data = vBase64
					}
				}
			}
		}
	}

	return data, rawData
}

type BulkTopicJson struct {
	ContentType     string            `json:"contentType"`
	EntryID         string            `json:"entryId"`
	Event           map[string]string `json:"event"`
	Data            json.RawMessage   `json:"data"`
	DataContentType string            `json:"datacontenttype"`
	ID              string            `json:"id"`
	PubsubName      string            `json:"pubsubname"`
	Source          string            `json:"source"`
	SpecVersion     string            `json:"specversion"`
	Time            string            `json:"time"`
	Topic           string            `json:"topic"`
	TraceID         string            `json:"traceid"`
	TraceParent     string            `json:"traceparent"`
	TraceState      string            `json:"tracestate"`
	Metadata        interface{}       `json:"metadata"`
}

// == APP == the item is  {application/cloudevents+json  5e582fd2-f1c4-47bd-81b1-803fb7e86552 map[data:multi-pong datacontenttype:text/plain id:92fc5348-097d-4b9f-b093-7e6fcda77add pubsubname:messages source:pub specversion:1.0 time:2023-12-02T11:42:31+05:30 topic:neworder traceid:00-a0373ef078e14e8db358c06e0ec18b27-d8dae6b5080eb9da-01 traceparent:00-a0373ef078e14e8db358c06e0ec18b27-d8dae6b5080eb9da-01 tracestate: type:com.dapr.event.sent] []           <nil>}

func (s *Server) AddBulkTopicEventHandler(sub *common.Subscription, fn common.BulkTopicEventHandler, maxMessagesCount, maxAwaitDurationMs int32) error {
	if sub == nil {
		return errors.New("subscription required")
	}
	// Route is only required for HTTP but should be specified for the
	// app protocol to be interchangeable.
	if sub.Route == "" {
		return errors.New("handler route name")
	}
	if err := s.topicRegistrar.AddBulkSubscription(sub, fn, maxMessagesCount, maxAwaitDurationMs); err != nil {
		return err
	}

	s.mux.Handle(sub.Route, optionsHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// check for post with no data
			var (
				body []byte
				err  error
			)
			if r.Body != nil {
				body, err = io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
					return
				}
			}
			if len(body) == 0 {
				http.Error(w, "nil content", PubSubHandlerDropStatusCode)
				return
			}

			// deserialize the event
			var ins map[string]interface{}
			if err = json.Unmarshal(body, &ins); err != nil {
				http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
				return
			}

			entriesInterface, ok := ins["entries"].([]interface{})
			if !ok {
				// Handle the error or return an error response
				http.Error(w, "Entries format error", PubSubHandlerDropStatusCode)
				return
			}

			statuses := make([]BulkSubscribeResponseEntry, 0, len(entriesInterface))

			var messages []common.BulkTopic
			for _, entry := range entriesInterface {
				itemMap, ok := entry.(map[string]interface{})
				if !ok {
					http.Error(w, "Entry format error", PubSubHandlerDropStatusCode)
					return
				}

				itemJSON, err := json.Marshal(itemMap["event"])
				if err != nil {
					http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
					return
				}
				var item BulkTopicJson

				if err := json.Unmarshal(itemJSON, &item); err != nil {
					http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
					return
				}
				data, rawData := item.getData()


				if item.PubsubName == "" {
					item.PubsubName = sub.PubsubName
				}

				if item.Topic == "" {
					item.Topic = sub.Topic
				}

				statuses = append(statuses, BulkSubscribeResponseEntry{
					entryId: item.EntryID,
					status:  SubscriptionResponseStatusSuccess,
				},
				)

				newItem := common.BulkTopic{
					ContentType:     item.ContentType,
					EntryID:         item.EntryID,
					Event:           item.Event,
					Data:            data,
					RawData:         rawData,
					DataContentType: item.DataContentType,
					ID:              item.EntryID,
					PubsubName:      item.PubsubName,
					Source:          item.Source,
					SpecVersion:     item.SpecVersion,
					Metadata:        item.Metadata,
					Time:            item.Time,
					Topic:           item.Topic,
					TraceID:         item.TraceID,
					TraceParent:     item.TraceParent,
					TraceState:      item.TraceState,
				}

				messages = append(messages, newItem)
			}
			resp := BulkSubscribeResponse{
				statuses: statuses,
			}
			responseJSON, err := json.Marshal(resp)
			if err != nil {
				http.Error(w, err.Error(), PubSubHandlerDropStatusCode)
				return
			}
			w.Header().Add("Content-Type", "application/json")
			w.Write(responseJSON)

			retry, err := fn(r.Context(), messages)
			if err == nil {
				writeStatus(w, common.SubscriptionResponseStatusSuccess)
				return
			}

			if retry {
				writeStatus(w, common.SubscriptionResponseStatusRetry)
				return
			}
			writeStatus(w, common.SubscriptionResponseStatusDrop)
		})))

	return nil
}

func writeStatus(w http.ResponseWriter, s string) {
	status := &common.SubscriptionResponse{Status: s}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, err.Error(), PubSubHandlerRetryStatusCode)
	}
}
