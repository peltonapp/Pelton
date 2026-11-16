package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// inlineLimit is the largest file handed to the frontend to show in place.
// Anything bigger is offered for saving instead.
const inlineLimit = 25 << 20

// cacheLimit is the largest download kept in the media cache.
const cacheLimit = 8 << 20

// cacheAge is how long a cached download is kept.
const cacheAge = 30 * 24 * time.Hour

// pruneEvery is how often a running session prunes the media cache.
const pruneEvery = 24 * time.Hour

// MediaContent is a file's bytes for showing in the app.
type MediaContent struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	// Data is the file, base64 encoded; empty when TooLarge.
	Data     string `json:"data"`
	TooLarge bool   `json:"tooLarge"`
}

// Progress reports bytes moved so far and the total, when known.
type Progress func(done, total int64)

// mediaSource is where one file of a message lives.
type mediaSource struct {
	uri  id.ContentURI
	file *attachment.EncryptedFile
	mime string
	// thumbW and thumbH ask the server for a thumbnail of uri; zero means
	// the file itself.
	thumbW, thumbH int
}

// sourceOf picks the file of a message to fetch: the thumbnail the sender
// attached, a server-made thumbnail of an unencrypted image, or the file.
func sourceOf(c *event.MessageEventContent, thumbnail bool) (mediaSource, error) {
	info := c.Info
	if thumbnail && info != nil {
		if info.ThumbnailFile != nil {
			uri, err := info.ThumbnailFile.URL.Parse()
			if err == nil {
				src := mediaSource{uri: uri, file: &info.ThumbnailFile.EncryptedFile}
				if info.ThumbnailInfo != nil {
					src.mime = info.ThumbnailInfo.MimeType
				}
				return src, nil
			}
		}
		if info.ThumbnailURL != "" {
			if uri, err := info.ThumbnailURL.Parse(); err == nil {
				src := mediaSource{uri: uri}
				if info.ThumbnailInfo != nil {
					src.mime = info.ThumbnailInfo.MimeType
				}
				return src, nil
			}
		}
	}
	var src mediaSource
	if info != nil {
		src.mime = info.MimeType
	}
	if c.File != nil {
		uri, err := c.File.URL.Parse()
		if err != nil {
			return src, fmt.Errorf("chat: bad file address: %w", err)
		}
		src.uri, src.file = uri, &c.File.EncryptedFile
		return src, nil
	}
	uri, err := c.URL.Parse()
	if err != nil {
		return src, fmt.Errorf("chat: bad file address: %w", err)
	}
	src.uri = uri
	if thumbnail && (c.MsgType == event.MsgImage || c.MsgType == "") {
		src.thumbW, src.thumbH = 800, 600
	}
	return src, nil
}

// messageContent loads a message's (decrypted) content.
func (s *session) messageContent(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*event.MessageEventContent, error) {
	row, ok, err := s.st.event(ctx, roomID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("chat: that message is not loaded")
	}
	if row.RedactedBy != "" {
		return nil, errors.New("chat: that message was removed")
	}
	evt, err := decodeEvent(row)
	if err != nil {
		return nil, err
	}
	dec, err := s.decrypt(ctx, evt)
	if err != nil {
		return nil, fmt.Errorf("chat: the message cannot be decrypted: %w", err)
	}
	c, ok := dec.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return nil, errors.New("chat: that message has no file")
	}
	// an edit may have replaced the file; the newest version wins.
	rels, err := s.st.relations(ctx, roomID, []id.EventID{eventID})
	if err == nil {
		for _, rel := range rels {
			if rel.RelType != string(event.RelReplace) || rel.RedactedBy != "" || rel.Sender != row.Sender {
				continue
			}
			if e, err := decodeEvent(rel); err == nil {
				if d, err := s.decrypt(ctx, e); err == nil {
					if ec, ok := d.Content.Parsed.(*event.MessageEventContent); ok && ec.NewContent != nil {
						c = ec.NewContent
					}
				}
			}
		}
	}
	return c, nil
}

// Media returns a message's file (or its thumbnail) for showing in the app.
func (m *Manager) Media(ctx context.Context, roomID, eventID string, thumbnail bool) (MediaContent, error) {
	s, err := m.active()
	if err != nil {
		return MediaContent{}, err
	}
	c, err := s.messageContent(ctx, id.RoomID(roomID), id.EventID(eventID))
	if err != nil {
		return MediaContent{}, err
	}
	src, err := sourceOf(c, thumbnail)
	if err != nil {
		return MediaContent{}, err
	}
	out := MediaContent{Name: c.GetFileName(), Mime: src.mime}
	if out.Name == "" {
		out.Name = c.Body
	}
	if c.Info != nil && !thumbnail {
		out.Size = int64(c.Info.Size)
		if out.Size > inlineLimit {
			out.TooLarge = true
			return out, nil
		}
	}
	data, mime, err := s.fetch(ctx, src)
	if err != nil {
		return MediaContent{}, err
	}
	if out.Mime == "" {
		out.Mime = mime
	}
	out.Size = int64(len(data))
	out.Data = base64.StdEncoding.EncodeToString(data)
	return out, nil
}

// Avatar returns a small square thumbnail of an mxc picture as a data URL.
func (m *Manager) Avatar(ctx context.Context, mxc string) (string, error) {
	return m.mxcImage(ctx, mxc, 96, 96)
}

// InlineImage returns an image that message HTML refers to by its mxc
// address, scaled to fit a message, as a data URL. Images anywhere but the
// homeserver are never fetched.
func (m *Manager) InlineImage(ctx context.Context, mxc string) (string, error) {
	return m.mxcImage(ctx, mxc, 800, 600)
}

func (m *Manager) mxcImage(ctx context.Context, mxc string, w, h int) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	uri, err := id.ParseContentURI(mxc)
	if err != nil {
		return "", fmt.Errorf("chat: %q is not a media address", mxc)
	}
	data, mime, err := s.fetch(ctx, mediaSource{uri: uri, thumbW: w, thumbH: h})
	if err != nil {
		return "", err
	}
	if mime == "" || !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(data)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// mediaClient is a client for the homeserver's media endpoints. It shares the
// session's sign-in but has no overall timeout, since a large file may take
// longer than any API call should.
func (s *session) mediaClient() *mautrix.Client {
	cli, _ := mautrix.NewClient(s.cli.HomeserverURL.String(), s.cli.UserID, s.cli.AccessToken)
	cli.DeviceID = s.cli.DeviceID
	cli.Client = s.media
	cli.UserAgent = userAgent
	cli.Log = s.cli.Log
	return cli
}

// download opens a file on the homeserver through the authenticated media
// endpoints.
func (s *session) download(ctx context.Context, src mediaSource) (*http.Response, error) {
	cli := s.mediaClient()
	if src.thumbW > 0 {
		return cli.DownloadThumbnail(ctx, src.uri, src.thumbH, src.thumbW, mautrix.DownloadThumbnailExtra{Method: "scale"})
	}
	return cli.Download(ctx, src.uri)
}

func (s *session) cachePath(src mediaSource) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", src.uri.String(), src.thumbW, src.thumbH)))
	return filepath.Join(s.m.opts.Dir, "media", hex.EncodeToString(sum[:]))
}

// fetch returns a file's bytes, decrypted when it is encrypted. Downloads up
// to cacheLimit are kept on disk as the server sent them, so an encrypted
// file stays encrypted in the cache.
func (s *session) fetch(ctx context.Context, src mediaSource) ([]byte, string, error) {
	path := s.cachePath(src)
	data, err := os.ReadFile(path)
	mime := src.mime
	if err != nil {
		resp, err := s.download(ctx, src)
		if err != nil {
			return nil, "", fmt.Errorf("chat: download: %w", err)
		}
		defer resp.Body.Close()
		if mime == "" && src.file == nil {
			mime = resp.Header.Get("Content-Type")
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, inlineLimit+1))
		if err != nil {
			return nil, "", fmt.Errorf("chat: download: %w", err)
		}
		if len(data) > inlineLimit {
			return nil, "", errors.New("chat: the file is too large to show")
		}
		if len(data) <= cacheLimit {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
				_ = os.WriteFile(path, data, 0o600)
			}
		}
	}
	if src.file != nil {
		plain, err := src.file.DecryptInPlaceReturn(bytes.Clone(data))
		if err != nil {
			_ = os.Remove(path)
			return nil, "", fmt.Errorf("chat: decrypt file: %w", err)
		}
		data = plain
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return data, mime, nil
}

// pruneMediaCacheDue prunes the media cache on its first call and then once
// pruneEvery has passed since the last prune.
func (s *session) pruneMediaCacheDue(now time.Time) {
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	s.lastPrune = now
	s.pruneMediaCache()
}

// pruneMediaCache removes cached downloads older than cacheAge.
func (s *session) pruneMediaCache() {
	dir := filepath.Join(s.m.opts.Dir, "media")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > cacheAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// SaveMedia writes a message's file to path, decrypting it on the way. The
// file appears at path only once it is complete and its hash checked.
func (m *Manager) SaveMedia(ctx context.Context, roomID, eventID, path string, progress Progress) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	c, err := s.messageContent(ctx, id.RoomID(roomID), id.EventID(eventID))
	if err != nil {
		return err
	}
	src, err := sourceOf(c, false)
	if err != nil {
		return err
	}
	resp, err := s.download(ctx, src)
	if err != nil {
		return fmt.Errorf("chat: download: %w", err)
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	if c.Info != nil && c.Info.Size > 0 {
		total = int64(c.Info.Size)
	}
	var body io.Reader = &countingReader{r: resp.Body, total: total, progress: progress}
	if src.file != nil {
		dec := src.file.DecryptStream(body)
		defer dec.Close()
		body = dec
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pelton-*")
	if err != nil {
		return fmt.Errorf("chat: save: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return fmt.Errorf("chat: save: %w", err)
	}
	if closer, ok := body.(io.Closer); ok {
		// the decrypting reader checks the file's hash when it is closed.
		if err := closer.Close(); err != nil {
			tmp.Close()
			return fmt.Errorf("chat: the file does not match what was sent: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// MediaFileName returns the file name a message's file was sent with.
func (m *Manager) MediaFileName(ctx context.Context, roomID, eventID string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	c, err := s.messageContent(ctx, id.RoomID(roomID), id.EventID(eventID))
	if err != nil {
		return "", err
	}
	name := c.GetFileName()
	if name == "" {
		name = c.Body
	}
	return filepath.Base(strings.ReplaceAll(name, "\\", "/")), nil
}

type countingReader struct {
	r        io.Reader
	done     int64
	total    int64
	progress Progress
	last     time.Time
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if c.progress != nil && (time.Since(c.last) > 100*time.Millisecond || err == io.EOF) {
		c.last = time.Now()
		c.progress(c.done, c.total)
	}
	return n, err
}

// UploadLimit returns the largest file the homeserver accepts, or 0 when it
// does not say.
func (m *Manager) UploadLimit(ctx context.Context) (int64, error) {
	s, err := m.active()
	if err != nil {
		return 0, err
	}
	return s.uploadLimitBytes(ctx), nil
}

func (s *session) uploadLimitBytes(ctx context.Context) int64 {
	s.mu.Lock()
	limit := s.uploadLimit
	s.mu.Unlock()
	if limit > 0 {
		return limit
	}
	cfg, err := s.cli.GetMediaConfig(ctx)
	if err != nil || cfg == nil {
		return 0
	}
	s.mu.Lock()
	s.uploadLimit = cfg.UploadSize
	s.mu.Unlock()
	return cfg.UploadSize
}

// FileRequest is a file to send.
type FileRequest struct {
	RoomID  string `json:"roomId"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	ReplyTo string `json:"replyTo"`
}

// ErrTooLarge is returned for a file over the server's upload limit.
var ErrTooLarge = errors.New("chat: the file is larger than the server accepts")

// thumbMax is the size images are shrunk to fit for their thumbnail.
const thumbMaxW, thumbMaxH = 800, 600

// SendFile uploads a file and posts it to a room. In an encrypted room the
// file and its thumbnail are encrypted before they leave the computer.
func (m *Manager) SendFile(ctx context.Context, req FileRequest, data []byte, progress Progress) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	if limit := s.uploadLimitBytes(ctx); limit > 0 && int64(len(data)) > limit {
		return "", fmt.Errorf("%w (%d MB)", ErrTooLarge, limit>>20)
	}
	roomID := id.RoomID(req.RoomID)
	encrypted, _ := s.stateStore.IsEncrypted(ctx, roomID)
	name := filepath.Base(strings.ReplaceAll(req.Name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	mime := req.Mime
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	mime, _, _ = strings.Cut(mime, ";")

	content := &event.MessageEventContent{
		MsgType:  msgTypeFor(mime),
		Body:     name,
		FileName: name,
		Info:     &event.FileInfo{MimeType: mime, Size: len(data)},
	}
	if content.MsgType == event.MsgImage {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			content.Info.Width, content.Info.Height = cfg.Width, cfg.Height
			if cfg.Width > thumbMaxW || cfg.Height > thumbMaxH {
				if thumb, tw, th, err := makeThumbnail(data); err == nil {
					uri, file, err := s.upload(ctx, thumb, "image/jpeg", "thumbnail.jpg", encrypted, nil)
					if err == nil {
						content.Info.ThumbnailInfo = &event.FileInfo{MimeType: "image/jpeg", Size: len(thumb), Width: tw, Height: th}
						if file != nil {
							content.Info.ThumbnailFile = &event.EncryptedFileInfo{EncryptedFile: *file, URL: uri.CUString()}
						} else {
							content.Info.ThumbnailURL = uri.CUString()
						}
					}
				}
			}
		}
	}
	uri, file, err := s.upload(ctx, data, mime, name, encrypted, progress)
	if err != nil {
		return "", err
	}
	if file != nil {
		content.File = &event.EncryptedFileInfo{EncryptedFile: *file, URL: uri.CUString()}
	} else {
		content.URL = uri.CUString()
	}
	if req.ReplyTo != "" {
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(id.EventID(req.ReplyTo))
	}
	resp, err := s.cli.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return "", fmt.Errorf("chat: send file: %w", err)
	}
	return resp.EventID.String(), nil
}

// upload puts bytes on the homeserver, encrypting them first when asked.
func (s *session) upload(ctx context.Context, data []byte, mime, name string, encrypt bool, progress Progress) (id.ContentURI, *attachment.EncryptedFile, error) {
	var file *attachment.EncryptedFile
	payload := data
	if encrypt {
		file = attachment.NewEncryptedFile()
		payload = file.Encrypt(data)
		mime = "application/octet-stream"
		name = ""
	}
	resp, err := s.mediaClient().UploadMedia(ctx, mautrix.ReqUploadMedia{
		Content:       &countingReader{r: bytes.NewReader(payload), total: int64(len(payload)), progress: progress},
		ContentLength: int64(len(payload)),
		ContentType:   mime,
		FileName:      name,
	})
	if err != nil {
		return id.ContentURI{}, nil, fmt.Errorf("chat: upload: %w", err)
	}
	return resp.ContentURI, file, nil
}

// msgTypeFor picks the message type for a file's MIME type.
func msgTypeFor(mime string) event.MessageType {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return event.MsgImage
	case strings.HasPrefix(mime, "video/"):
		return event.MsgVideo
	case strings.HasPrefix(mime, "audio/"):
		return event.MsgAudio
	}
	return event.MsgFile
}

// makeThumbnail shrinks an image to fit thumbMaxW by thumbMaxH as a JPEG.
func makeThumbnail(data []byte) ([]byte, int, int, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("chat: empty image")
	}
	scale := min(float64(thumbMaxW)/float64(w), float64(thumbMaxH)/float64(h), 1)
	tw, th := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	// jpeg has no transparency; what was see-through shows white.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), tw, th, nil
}
