package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSESNotifier struct {
	mock.Mock
}

func (m *mockSESNotifier) SendEmail(
	ctx context.Context,
	params *ses.SendEmailInput,
	optFns ...func(*ses.Options),
) (*ses.SendEmailOutput, error) {
	args := m.Called(ctx, params, optFns)
	if output := args.Get(0); output != nil {
		return output.(*ses.SendEmailOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

type mockNotificationConfig struct {
	mock.Mock
}

func (m *mockNotificationConfig) GetSESAddressFrom() string {
	return m.Called().String(0)
}

type mockLogger struct{}

func (*mockLogger) Debug(string, ...any) {}
func (*mockLogger) Info(string, ...any)  {}
func (*mockLogger) Warn(string, ...any)  {}
func (*mockLogger) Error(string, ...any) {}

func TestStatusNotifierNotifySendsEmailToEachDestination(t *testing.T) {
	client := new(mockSESNotifier)
	cfg := new(mockNotificationConfig)
	cfg.On("GetSESAddressFrom").Return("notifications@example.com").Twice()
	inputs := make([]*ses.SendEmailInput, 0, 2)
	client.
		On("SendEmail", t.Context(), mock.AnythingOfType("*ses.SendEmailInput"), mock.Anything).
		Run(func(args mock.Arguments) {
			inputs = append(inputs, args.Get(1).(*ses.SendEmailInput))
		}).
		Return(&ses.SendEmailOutput{MessageId: new("message-id")}, nil).
		Twice()

	notifier := &StatusNotifier{
		client: client,
		cfg:    cfg,
		logger: new(mockLogger),
	}
	subject := "Processing complete"
	content := "Your itinerary is ready"
	destinations := []string{"first@example.com", "second@example.com"}

	err := notifier.Notify(t.Context(), subject, destinations, &content)

	require.NoError(t, err)
	require.Len(t, inputs, 2)
	for index, input := range inputs {
		require.NotNil(t, input.Destination)
		assert.Equal(t, []string{destinations[index]}, input.Destination.ToAddresses)

		require.NotNil(t, input.Message)
		require.NotNil(t, input.Message.Subject)
		assert.Equal(t, subject, *input.Message.Subject.Data)

		require.NotNil(t, input.Message.Body)
		require.NotNil(t, input.Message.Body.Text)
		assert.Equal(t, content, *input.Message.Body.Text.Data)

		assert.Equal(t, "notifications@example.com", *input.Source)
	}
	client.AssertExpectations(t)
	cfg.AssertExpectations(t)
}

func TestStatusNotifierNotifyContinuesAndJoinsSendErrors(t *testing.T) {
	firstErr := errors.New("first send failed")
	secondErr := errors.New("second send failed")
	client := new(mockSESNotifier)
	cfg := new(mockNotificationConfig)
	cfg.On("GetSESAddressFrom").Return("notifications@example.com").Twice()
	client.
		On("SendEmail", t.Context(), mock.AnythingOfType("*ses.SendEmailInput"), mock.Anything).
		Return(nil, firstErr).
		Once()
	client.
		On("SendEmail", t.Context(), mock.AnythingOfType("*ses.SendEmailInput"), mock.Anything).
		Return(nil, secondErr).
		Once()

	notifier := &StatusNotifier{
		client: client,
		cfg:    cfg,
		logger: new(mockLogger),
	}
	content := "Your itinerary could not be processed"

	err := notifier.Notify(
		t.Context(),
		"Processing failed",
		[]string{"first@example.com", "second@example.com"},
		&content,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.ErrorContains(t, err, "f***@example.com")
	require.ErrorContains(t, err, "s***@example.com")
	client.AssertExpectations(t)
	cfg.AssertExpectations(t)
}

func TestStatusNotifierNotifyWithNoDestinationsDoesNotSend(t *testing.T) {
	client := new(mockSESNotifier)
	notifier := &StatusNotifier{
		client: client,
		cfg:    new(mockNotificationConfig),
		logger: new(mockLogger),
	}
	content := "Your itinerary is ready"

	err := notifier.Notify(t.Context(), "Processing complete", nil, &content)
	assert.Equal(t, ErrNoDestinationProvided, err)
}

func TestMaskEmailAddress(t *testing.T) {
	tests := []struct {
		input  string
		output string
	}{
		{input: "hello@gmail.com", output: "h***@gmail.com"},
		{input: "my_email@example.com", output: "m***@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			out, err := maskEmailAddress(tt.input)

			require.NoError(t, err)
			assert.Equal(t, tt.output, out)
		})
	}
}

func TestMaskEmailAddressEmpty(t *testing.T) {
	_, err := maskEmailAddress("")
	assert.Error(t, err, "email was empty")
}

func TestMaskEmailAddressNoAtSymbol(t *testing.T) {
	_, err := maskEmailAddress("hello")
	assert.Error(t, err, "did not have @")
}
