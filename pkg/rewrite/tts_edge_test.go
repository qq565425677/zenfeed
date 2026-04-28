package rewrite

import (
	"context"
	"errors"
	"testing"

	"github.com/difyz9/edge-tts-go/pkg/communicate"
)

func TestBuildEdgeTTSSegments(t *testing.T) {
	t.Run("split by speaker and merge consecutive same voice", func(t *testing.T) {
		transcript := `- Alice
- Bob

Followed by the actual dialogue script:
Alice: Hello.
Alice: How are you?
Bob：I am good.
Other: Unknown speaker line.`
		segments, err := buildEdgeTTSSegments(transcript, []Speaker{
			{Name: "Alice", Voice: "en-US-AnaNeural"},
			{Name: "Bob", Voice: "en-US-GuyNeural"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segments) != 3 {
			t.Fatalf("expected 3 segments, got %d", len(segments))
		}
		if segments[0].voice != "en-US-AnaNeural" {
			t.Fatalf("expected first segment voice en-US-AnaNeural, got %s", segments[0].voice)
		}
		if segments[0].text != "Hello.\nHow are you?" {
			t.Fatalf("unexpected first segment text: %q", segments[0].text)
		}
		if segments[1].voice != "en-US-GuyNeural" {
			t.Fatalf("expected second segment voice en-US-GuyNeural, got %s", segments[1].voice)
		}
		if segments[1].text != "I am good." {
			t.Fatalf("unexpected second segment text: %q", segments[1].text)
		}
		if segments[2].voice != "en-US-AnaNeural" {
			t.Fatalf("expected unknown speaker to fallback to default voice, got %s", segments[2].voice)
		}
	})

	t.Run("fallback to whole transcript when no lines are parsed", func(t *testing.T) {
		segments, err := buildEdgeTTSSegments("   ", []Speaker{{Name: "A", Voice: "v"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segments) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segments))
		}
		if segments[0].text != "   " {
			t.Fatalf("unexpected segment text: %q", segments[0].text)
		}
	})

	t.Run("error on empty speakers", func(t *testing.T) {
		_, err := buildEdgeTTSSegments("Alice: hello", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStripLeadingID3Tag(t *testing.T) {
	// ID3 header with zero payload size.
	data := append([]byte{'I', 'D', '3', 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, []byte("AUDIO")...)
	stripped := stripLeadingID3Tag(data)
	if string(stripped) != "AUDIO" {
		t.Fatalf("expected AUDIO, got %q", string(stripped))
	}

	plain := []byte("NO-ID3")
	if string(stripLeadingID3Tag(plain)) != "NO-ID3" {
		t.Fatalf("plain bytes should not be modified")
	}
}

func TestEdgeTTSSingleRetriesTransientFailure(t *testing.T) {
	originalFactory := newEdgeTTSCommunicate
	t.Cleanup(func() {
		newEdgeTTSCommunicate = originalFactory
	})

	attempts := 0
	newEdgeTTSCommunicate = func(string, string, string, string, string, string, int, int, ...string) (*communicate.Communicate, error) {
		attempts++
		return nil, errors.New("boom")
	}

	_, err := edgeTTSSingle(context.Background(), "hello", "zh-CN-XiaoxiaoNeural")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != edgeTTSRetryAttempts {
		t.Fatalf("expected %d attempts, got %d", edgeTTSRetryAttempts, attempts)
	}
}

func TestEdgeTTSErrorSnippet(t *testing.T) {
	if got := edgeTTSErrorSnippet("  hello  "); got != "hello" {
		t.Fatalf("unexpected snippet: %q", got)
	}

	long := ""
	for i := 0; i < edgeTTSErrorSnippetRunes+10; i++ {
		long += "测"
	}
	got := edgeTTSErrorSnippet(long)
	if len([]rune(got)) != edgeTTSErrorSnippetRunes+3 {
		t.Fatalf("unexpected snippet rune length: %d", len([]rune(got)))
	}
}
