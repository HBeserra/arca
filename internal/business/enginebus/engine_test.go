package enginebus_test

import (
	"changeme/internal/business/enginebus"
	enginemock_test "changeme/internal/business/enginebus/mocks"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestEngine_EmbedFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	storeMock := enginemock_test.NewMockStore(ctrl)

	engine, err := enginebus.New(slog.Default(), storeMock)
	require.NoError(t, err)
	defer engine.Close(ctx)

	engine.Load(ctx)

	sessionID := uuid.New()
	session := enginebus.Session{
		ID:            sessionID,
		BatchSize:     4096,
		BatchsOverlap: 512,
	}

	doc := enginebus.AddDocumentInput{
		Name:        "Test Document",
		SessionID:   sessionID,
		Path:        "",
		ContentType: "",
		Text:        "This is the password for the secret vault: correcthorsebatterystaple",
	}

	storeMock.EXPECT().GetSession(ctx, sessionID).Return(session, nil)
	storeMock.EXPECT().CreateDocument(ctx, gomock.Any()).Return(nil)
	storeMock.EXPECT().AddDocumentChunk(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	err = engine.AddDocumentText(ctx, doc)
	require.NoError(t, err)

}

func TestEngine_AskWithDocs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	storeMock := enginemock_test.NewMockStore(ctrl)

	engine, err := enginebus.New(slog.Default(), storeMock,
		enginebus.WithChatModel("unsloth/Qwen3-0.6B-GGUF/Qwen3-0.6B-Q8_0.gguf"),
	)
	require.NoError(t, err)
	defer engine.Close(ctx)

	engine.Load(ctx)

	sessionID := uuid.New()
	session := enginebus.Session{
		ID:            sessionID,
		BatchSize:     4096,
		BatchsOverlap: 512,
	}

	storeMock.EXPECT().GetSession(gomock.Any(), sessionID).Return(session, nil)
	storeMock.EXPECT().UpdateSession(gomock.Any(), gomock.Any()).Return(nil)

	response, err := engine.Chat(ctx, enginebus.Question{
		SessionID: sessionID,
		Content:   "What is the password for the secret vault?",
		Fragments: []enginebus.Fragment{
			{
				Path: "vault.txt",
				Text: "This is the password for the secret vault: correcthorsebatterystaple",
			},
		},
	})
	require.NoError(t, err)
	require.Contains(t, response.Content, "correcthorsebatterystaple")
}

func TestEngine_AskWithDocsStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	storeMock := enginemock_test.NewMockStore(ctrl)

	engine, err := enginebus.New(slog.Default(), storeMock,
		enginebus.WithChatModel("unsloth/Qwen3-0.6B-GGUF/Qwen3-0.6B-Q8_0.gguf"),
	)
	require.NoError(t, err)
	defer engine.Close(ctx)

	engine.Load(ctx)

	sessionID := uuid.New()
	session := enginebus.Session{
		ID:            sessionID,
		BatchSize:     4096,
		BatchsOverlap: 512,
	}

	storeMock.EXPECT().GetSession(gomock.Any(), sessionID).Return(session, nil)
	storeMock.EXPECT().UpdateSession(gomock.Any(), gomock.Any()).Return(nil)

	events, err := engine.ChatStream(ctx, enginebus.Question{
		SessionID: sessionID,
		Content:   "What is the password for the secret vault?",
		Fragments: []enginebus.Fragment{
			{
				Path: "vault.txt",
				Text: "This is the password for the secret vault: correcthorsebatterystaple",
			},
		},
	})
	require.NoError(t, err)

	var tokens strings.Builder
	var answer *enginebus.Answer

	for event := range events {
		require.NoError(t, event.Err)
		if event.Answer != nil {
			answer = event.Answer
		} else {
			tokens.WriteString(event.Token)
		}
	}

	require.NotNil(t, answer)
	require.Equal(t, tokens.String(), answer.Content)
	require.Contains(t, answer.Content, "correcthorsebatterystaple")
}
