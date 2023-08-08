package blockyemail

import (
	"context"
	"errors"
	"io"

	"github.com/bufbuild/connect-go"
	"gocloud.dev/gcerrors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/blockysource/blocky-cloud/email"
	"github.com/blockysource/blocky-cloud/email/driver"
	mailingv1alpha "github.com/blockysource/go-genproto/blocky/mailing/v1alpha"
	"github.com/blockysource/go-genproto/blocky/mailing/v1alpha/mailingv1alphaconnect"
)

// URLOpener represents an url opener for blocky email service implementation.
type URLOpener struct {
	// GRPCClient is the gRPC client for the mailing service.
	// This is required for the default gRPC implementation.
	GRPCClient mailingv1alpha.MailingServiceClient
	// ConnectClient is the gRPC client for the mailing service.
	// This client is used for the connect implementation.
	ConnectClient mailingv1alphaconnect.MailingServiceClient
}

// SenderOptions represents options for the sender.
type SenderOptions struct {
	AttachmentOpts AttachmentOptions
}

// OpenGRPCMailingSender opens a new gRPC mailing sender.
func OpenGRPCMailingSender(client mailingv1alpha.MailingServiceClient, opts SenderOptions, callOpts ...grpc.CallOption) (*email.Sender, error) {
	if client == nil {
		return nil, errors.New("blockyemail: mailing service client is required")
	}

	return email.NewSender(&gRPCSender{
		gc:            client,
		senderOptions: opts,
		callOpts:      callOpts,
	}, nil), nil
}

// func OpenConnectMailingSender(cc mailingv1alphaconnect.MailingServiceClient, opts SenderOptions) (*email.Sender, error) {
// 	res, _ := cc.UploadAttachment().CloseAndReceive()
//
// }

var _ driver.Sender = (*gRPCSender)(nil)

type gRPCSender struct {
	gc            mailingv1alpha.MailingServiceClient
	senderOptions SenderOptions
	callOpts      []grpc.CallOption
}

// SendBatch sends a batch of messages.
func (g *gRPCSender) SendBatch(ctx context.Context, msgs []*driver.Message) error {
	for _, m := range msgs {
		var attachments []*mailingv1alpha.EmailAttachment
		for _, att := range m.Attachments {
			if err := g.uploadAttachment(ctx, att); err != nil {
				return err
			}
			attachments = append(attachments, &mailingv1alpha.EmailAttachment{
				AttachmentId: att.ContentID,
			})
		}
		msg := driverMessageToProto(m, attachments)

		if m.BeforeSend != nil {
			if err := m.BeforeSend(func(i any) bool {
				if as, ok := i.(**mailingv1alpha.Message); ok {
					*as = &msg
					return true
				}
				return false
			}); err != nil {
				return err
			}
		}
		req := mailingv1alpha.SendMessageRequest{Message: &msg}
		res, err := g.gc.SendMessage(ctx, &req, g.callOpts...)
		if err != nil {
			return err
		}
		m.MessageID = res.MessageId

		if m.AfterSend != nil {
			if err = m.AfterSend(func(i any) bool {
				if as, ok := i.(**mailingv1alpha.SendMessageResponse); ok {
					*as = res
					return true
				}
				return false
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func driverMessageToProto(m *driver.Message, attachments []*mailingv1alpha.EmailAttachment) mailingv1alpha.Message {
	msg := mailingv1alpha.Message{
		MessageId:   m.MessageID,
		Subject:     m.Subject,
		Html:        m.HtmlBody,
		Text:        m.TextBody,
		Attachments: attachments,
	}

	for _, to := range m.To {
		msg.To = append(msg.To, to.String())
	}

	for _, cc := range m.Cc {
		msg.Cc = append(msg.Cc, cc.String())
	}

	for _, bcc := range m.Bcc {
		msg.Bcc = append(msg.Bcc, bcc.String())
	}

	if m.ReplyTo != nil {
		msg.ReplyTo = m.ReplyTo.String()
	}
	if !m.Date.IsZero() {
		msg.Date = timestamppb.New(m.Date)
	}
	return msg
}

func (g *gRPCSender) ErrorAs(err error, i interface{}) bool {
	if s, ok := status.FromError(err); ok {
		if as, ok := i.(**status.Status); ok {
			*as = s
			return true
		}
	}

	return false
}

func (g *gRPCSender) ErrorCode(err error) codes.Code {
	return gcerrors.ErrorCode(status.Code(err))
}

func (g *gRPCSender) As(i interface{}) bool {
	if as, ok := i.(*mailingv1alpha.MailingServiceClient); ok {
		*as = g.gc
		return true
	}
	return false
}

func (g *gRPCSender) Close() error {
	return nil
}

// AttachmentOptions represents options for uploading attachments.
type AttachmentOptions struct {
	ChunkSize int
}

const defaultChunkSize = 64 * 1024

func (g *gRPCSender) uploadAttachment(ctx context.Context, att *driver.Attachment) error {
	header := mailingv1alpha.UploadAttachmentHeader{
		AttachmentId: att.ContentID,
		FileName:     att.Filename,
		ContentType:  att.ContentType,
	}

	stream, err := g.gc.UploadAttachment(ctx, g.callOpts...)
	if err != nil {
		return err
	}

	req := mailingv1alpha.UploadAttachmentRequest{Attachment: &mailingv1alpha.UploadAttachmentRequest_Header{Header: &header}}

	if err = stream.Send(&req); err != nil {
		return err
	}

	chunkSize := g.senderOptions.AttachmentOpts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	chunk := make([]byte, chunkSize)

	chunkMsg := mailingv1alpha.UploadAttachmentRequest_Chunk{}

	var n, bytesRead int
	for {
		var eof bool
		n, err = att.File.Read(chunk)
		if err == io.EOF {
			eof = true
		} else if err != nil {
			return err
		}
		bytesRead += n

		cd := mailingv1alpha.UploadAttachmentChunk{Data: chunk[:n]}
		chunkMsg.Chunk = &cd
		req = mailingv1alpha.UploadAttachmentRequest{Attachment: &chunkMsg}

		if err = stream.Send(&req); err != nil {
			return err
		}

		if eof {
			break
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	if resp.Size != uint32(bytesRead) {
		return errors.New("size mismatch")
	}

	att.ContentID = resp.AttachmentId
	return nil
}

type connectSender struct {
	cc             mailingv1alphaconnect.MailingServiceClient
	attachmentOpts AttachmentOptions
	callOpts       []grpc.CallOption
}

func (g *connectSender) SendBatch(ctx context.Context, msgs []*driver.Message) error {
	for _, m := range msgs {
		var attachments []*mailingv1alpha.EmailAttachment
		for _, att := range m.Attachments {
			if err := uploadConnectAttachment(ctx, g.cc, att, g.attachmentOpts, g.callOpts...); err != nil {
				return err
			}
			attachments = append(attachments, &mailingv1alpha.EmailAttachment{
				AttachmentId: att.ContentID,
			})
		}
		msg := mailingv1alpha.Message{
			MessageId:   m.MessageID,
			Subject:     m.Subject,
			Html:        m.HtmlBody,
			Text:        m.TextBody,
			Attachments: attachments,
		}

		for _, to := range m.To {
			msg.To = append(msg.To, to.String())
		}

		for _, cc := range m.Cc {
			msg.Cc = append(msg.Cc, cc.String())
		}

		for _, bcc := range m.Bcc {
			msg.Bcc = append(msg.Bcc, bcc.String())
		}

		if m.ReplyTo != nil {
			msg.ReplyTo = m.ReplyTo.String()
		}
		if !m.Date.IsZero() {
			msg.Date = timestamppb.New(m.Date)
		}

		if m.BeforeSend != nil {
			if err := m.BeforeSend(func(i any) bool {
				if as, ok := i.(**mailingv1alpha.Message); ok {
					*as = &msg
					return true
				}
				return false
			}); err != nil {
				return err
			}
		}
		req := mailingv1alpha.SendMessageRequest{Message: &msg}
		res, err := g.cc.SendMessage(ctx, connect.NewRequest(&req))
		if err != nil {
			return err
		}
		m.MessageID = res.Msg.MessageId

		if m.AfterSend != nil {
			if err = m.AfterSend(func(i any) bool {
				if as, ok := i.(**mailingv1alpha.SendMessageResponse); ok {
					*as = res.Msg
					return true
				}
				return false
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func uploadConnectAttachment(ctx context.Context, gc mailingv1alphaconnect.MailingServiceClient, att *driver.Attachment, opts AttachmentOptions, callOpts ...grpc.CallOption) error {
	header := mailingv1alpha.UploadAttachmentHeader{
		AttachmentId: att.ContentID,
		FileName:     att.Filename,
		ContentType:  att.ContentType,
	}

	stream := gc.UploadAttachment(ctx)

	req := mailingv1alpha.UploadAttachmentRequest{Attachment: &mailingv1alpha.UploadAttachmentRequest_Header{Header: &header}}

	if err := stream.Send(&req); err != nil {
		return err
	}

	chunk := make([]byte, opts.ChunkSize)

	chunkMsg := mailingv1alpha.UploadAttachmentRequest_Chunk{}

	var (
		n, bytesRead int
		err          error
	)
	for {
		var eof bool
		n, err = att.File.Read(chunk)
		if err == io.EOF {
			eof = true
		} else if err != nil {
			return err
		}
		bytesRead += n

		cd := mailingv1alpha.UploadAttachmentChunk{Data: chunk[:n]}
		chunkMsg.Chunk = &cd
		req = mailingv1alpha.UploadAttachmentRequest{Attachment: &chunkMsg}

		if err = stream.Send(&req); err != nil {
			return err
		}

		if eof {
			break
		}
	}

	resp, err := stream.CloseAndReceive()
	if err != nil {
		return err
	}

	if resp.Msg.Size != uint32(bytesRead) {
		return errors.New("size mismatch")
	}

	att.ContentID = resp.Msg.AttachmentId
	return nil
}
