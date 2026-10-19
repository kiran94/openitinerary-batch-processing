package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	oibplogging "github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Notifier interface {
	Notify(ctx context.Context, subject string, destination []string, content *string) error
}

func NewNotifier(ctx context.Context, cfg config.Config, logger logging.Logger, tracer trace.Tracer) (Notifier, error) {
	c, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	cfg.GetSESAddressFrom()
	cfg.GetSESConfigurationSetName()
	otelaws.AppendMiddlewares(&c.APIOptions)

	client := ses.NewFromConfig(c)
	return &StatusNotifier{client, cfg, logger, tracer}, nil
}

type sesNotifier interface {
	SendEmail(ctx context.Context, params *ses.SendEmailInput, optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error)
}

type notificationConfig interface {
	GetSESAddressFrom() string
}

type StatusNotifier struct {
	client sesNotifier
	cfg    notificationConfig
	logger logging.Logger
	tracer trace.Tracer
}

var ErrNoDestinationProvided = errors.New("no destination was provided")

func (n *StatusNotifier) Notify(ctx context.Context, subject string, destination []string, content *string) error {
	ctx, span := n.tracer.Start(ctx, string(telemetry.TracingSpanSendNotification), trace.WithAttributes(attribute.KeyValue{Key: "recipient.count", Value: attribute.IntValue(len(destination))}))
	defer span.End()

	logger := n.logger.WithContext(ctx)

	if len(destination) == 0 {
		span.RecordError(ErrNoDestinationProvided)
		return ErrNoDestinationProvided
	}

	/*
		Send to each individual destination because if
		one of the user provided emails are invalid then all recipients
		will not get the message
		https://docs.aws.amazon.com/ses/latest/dg/send-email.html
	*/
	sendErrors := make([]error, 0, len(destination))
	for _, dest := range destination {

		maskedDest, err := maskEmailAddress(dest)
		if err != nil {
			sendErrors = append(sendErrors, err)
			continue
		}

		output, err := n.client.SendEmail(ctx, &ses.SendEmailInput{
			Destination: &types.Destination{ToAddresses: []string{dest}},
			Message: &types.Message{
				Subject: &types.Content{Data: &subject},
				Body:    &types.Body{Text: &types.Content{Data: content}},
			},
			Source: new(n.cfg.GetSESAddressFrom()),
		})

		if err != nil {
			err = errors.Join(err, fmt.Errorf("could not send %v notification to %v", subject, maskedDest))
			sendErrors = append(sendErrors, err)
			continue
		}

		logger.Debug(ctx,
			"successfully sent email",
			"component", oibplogging.ComponentNotifier,
			"operation", oibplogging.OperationNotificationSend,
			"destination", maskedDest,
			"ses_message_id", output.MessageId,
		)
	}

	if err := errors.Join(sendErrors...); err != nil {
		return err
	}

	return nil
}

func maskEmailAddress(email string) (string, error) {
	if email == "" {
		return "", errors.New("provided email was empty")
	}

	AtSymbolPosition := strings.IndexRune(email, '@')
	if AtSymbolPosition == -1 {
		return "", errors.New("provided email did not have @")
	}

	runes := []rune(email)
	firstRune := runes[0]
	domain := email[AtSymbolPosition+1:]

	return fmt.Sprintf("%v***@%v", string(firstRune), domain), nil
}
