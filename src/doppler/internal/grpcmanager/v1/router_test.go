package v1_test

import (
	"doppler/internal/grpcmanager/v1"
	"plumbing"

	. "github.com/apoydence/eachers"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Router", func() {
	var (
		counterEnvelope      *events.Envelope
		logEnvelope          *events.Envelope
		counterEnvelopeBytes []byte
		logEnvelopeBytes     []byte

		router *v1.Router
	)

	BeforeEach(func() {
		counterEnvelope = &events.Envelope{
			Origin:    proto.String("some-origin"),
			EventType: events.Envelope_CounterEvent.Enum(),
		}

		logEnvelope = &events.Envelope{
			Origin:    proto.String("some-origin"),
			EventType: events.Envelope_LogMessage.Enum(),
		}
		var err error
		counterEnvelopeBytes, err = counterEnvelope.Marshal()
		Expect(err).ToNot(HaveOccurred())

		logEnvelopeBytes, err = logEnvelope.Marshal()
		Expect(err).ToNot(HaveOccurred())

		router = v1.NewRouter()
	})

	Context("with firehose subscriptions", func() {
		var (
			// Firehoses
			multipleFirehoseOfSameSubscription []*mockDataSetter
			singleFirehoseSubscription         *mockDataSetter

			requestForMultipleSubscriptions *plumbing.SubscriptionRequest
			requestForSingleSubscription    *plumbing.SubscriptionRequest

			cleanupSingleFirehose func()
		)

		BeforeEach(func() {
			multipleFirehoseOfSameSubscription = []*mockDataSetter{
				newMockDataSetter(),
				newMockDataSetter(),
			}
			singleFirehoseSubscription = newMockDataSetter()

			requestForMultipleSubscriptions = &plumbing.SubscriptionRequest{
				ShardID: "some-sub-id",
			}

			requestForSingleSubscription = &plumbing.SubscriptionRequest{
				ShardID: "some-other-sub-id",
			}
			// Firehose
			router.Register(requestForMultipleSubscriptions, multipleFirehoseOfSameSubscription[0])
			router.Register(requestForMultipleSubscriptions, multipleFirehoseOfSameSubscription[1])
			cleanupSingleFirehose = router.Register(requestForSingleSubscription, singleFirehoseSubscription)
		})

		It("receives all messages", func() {
			router.SendTo("some-app-id", logEnvelope)
			router.SendTo("some-app-id", counterEnvelope)

			Expect(singleFirehoseSubscription.SetInput).To(
				BeCalled(With(logEnvelopeBytes)),
			)
			Expect(singleFirehoseSubscription.SetInput).To(
				BeCalled(With(counterEnvelopeBytes)),
			)
		})

		Context("when there are two subscriptions with the same ID", func() {
			It("sends the message to one subscription", func() {
				router.SendTo("some-app-id", logEnvelope)
				combinedLen := len(multipleFirehoseOfSameSubscription[0].SetCalled) + len(multipleFirehoseOfSameSubscription[1].SetCalled)

				Expect(combinedLen).To(Equal(1))
			})
		})

		It("does not send messages to unregistered firehose subscriptions", func() {
			cleanupSingleFirehose()
			router.SendTo("some-app-id", counterEnvelope)

			Expect(singleFirehoseSubscription.SetInput).To(
				Not(BeCalled()),
			)
		})

		It("ignores invalid envelopes", func() {
			router.SendTo("some-app-id", new(events.Envelope))

			Expect(singleFirehoseSubscription.SetCalled).To(
				Not(BeCalled()),
			)
		})
	})

	Context("with app id subscriptions", func() {
		var (
			streamForAppA              *mockDataSetter
			streamForAppB              *mockDataSetter
			subscriptionRequestForAppA *plumbing.SubscriptionRequest
			subscriptionRequestForAppC *plumbing.SubscriptionRequest

			cleanupForAppA func()
			cleanupForAppC func()
		)

		BeforeEach(func() {
			streamForAppA = newMockDataSetter()
			streamForAppB = newMockDataSetter()

			subscriptionRequestForAppA = &plumbing.SubscriptionRequest{
				Filter: &plumbing.Filter{
					AppID: "some-app-id",
				},
			}

			subscriptionRequestForAppC = &plumbing.SubscriptionRequest{
				Filter: &plumbing.Filter{
					AppID: "some-other-app-id",
				},
			}
			// Streams without type filters
			cleanupForAppA = router.Register(subscriptionRequestForAppA, streamForAppA)
			cleanupForAppC = router.Register(subscriptionRequestForAppC, streamForAppB)
		})

		It("survives the race detector for thread safety", func(done Done) {
			cleanup := router.Register(subscriptionRequestForAppA, streamForAppA)

			go func() {
				defer close(done)
				router.SendTo("some-app-id", counterEnvelope)
			}()
			cleanup()
		})

		It("sends a message to the subscription with the same app id", func() {
			router.SendTo("some-app-id", counterEnvelope)

			Expect(streamForAppA.SetInput).To(
				BeCalled(With(counterEnvelopeBytes)),
			)
		})

		It("sends the envelope to a subscription only once", func() {
			router.SendTo("some-app-id", counterEnvelope)

			Expect(streamForAppA.SetCalled).To(
				HaveLen(1),
			)
		})

		It("does not send data to that setter", func() {
			cleanupForAppA()
			router.SendTo("some-app-id", counterEnvelope)

			Expect(streamForAppA.SetCalled).To(
				Not(BeCalled()),
			)
		})

		It("does not send the message to a subscription of a different app id", func() {
			router.SendTo("some-app-id", counterEnvelope)

			Expect(streamForAppB.SetInput).To(
				Not(BeCalled()),
			)
		})
	})

	Context("with log filter subscriptions", func() {
		var (
			streamWithLogFilter *mockDataSetter
			subscriptionRequest *plumbing.SubscriptionRequest
		)

		BeforeEach(func() {
			streamWithLogFilter = newMockDataSetter()

			subscriptionRequest = &plumbing.SubscriptionRequest{
				Filter: &plumbing.Filter{
					AppID: "some-app-id",
					Message: &plumbing.Filter_Log{
						Log: &plumbing.LogFilter{},
					},
				},
			}

			router.Register(subscriptionRequest, streamWithLogFilter)
		})

		It("sends only log messages", func() {
			router.SendTo("some-app-id", counterEnvelope)

			Expect(streamWithLogFilter.SetCalled).To(
				Not(BeCalled()),
			)

			router.SendTo("some-app-id", logEnvelope)

			Expect(streamWithLogFilter.SetInput).To(
				BeCalled(With(logEnvelopeBytes)),
			)
		})
	})
})
