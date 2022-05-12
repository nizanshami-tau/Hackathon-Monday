package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// const DB_PATH string = "/var/local/whatsapp-broker/app.db"
const DB_PATH string = "/tmp/myapp.db"
const REDIRECT_PATH string = "https://sunday.sviry.net"
const CLIENT_ID string = "63096c5c98c7077e0a8db84a4a21b299"
const CLIENT_SECRET string = "90d0e45f578ec3d092c4806674dd4033"

type WhatsappService struct {
	container *sqlstore.Container
	logger    waLog.Logger
	idToQr    sync.Map
}

func NewWhatsappService() (*WhatsappService, error) {
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	container, err := sqlstore.New("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", DB_PATH), dbLog)
	if err != nil {
		return nil, err
	}

	return &WhatsappService{
		container: container,
		logger:    waLog.Stdout("Service", "DEBUG", true),
	}, nil
}

func (s *WhatsappService) SendWhatsappQR(w http.ResponseWriter, req *http.Request) {
	deviceStore := s.container.NewDevice()

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	qrCtx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	qrChan, _ := client.GetQRChannel(qrCtx)
	err := client.Connect()
	if err != nil {
		clientLog.Errorf("Connect error: %s", err)
		w.WriteHeader(500)
		return
	}

	clientLog.Debugf("Waiting on qr channel")
	evt := <-qrChan
	clientLog.Debugf("QR channel called with event: %+v", evt)
	if evt.Event == "code" {
		imgBuf := &bytes.Buffer{}
		cmd := exec.CommandContext(req.Context(), "bash", "-c", "qrencode -t png -o - | base64")
		cmd.Stdin = bufio.NewReader(strings.NewReader(evt.Code))
		cmd.Stdout = imgBuf

		err := cmd.Run()
		if err != nil {

			clientLog.Errorf("Error rendering qr: '%s', %v, %v", evt.Code, err)
			w.WriteHeader(500)
			return
		}

		id, err := uuid.NewRandom()
		if err != nil {
			clientLog.Errorf("Error generating id: '%s', %v, %v", evt.Code, err)
			w.WriteHeader(500)
			return
		}

		go func() {
			<-qrCtx.Done()
			s.idToQr.Delete(id)
		}()

		if _, loaded := s.idToQr.LoadOrStore(id, qrChan); loaded {
			clientLog.Errorf("Login session id %s already exists in mapping", id.String())
			w.WriteHeader(500)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html>
	<head>
		<title>Whatsapp QR login</title>
	</head>
	<body>
		<img src="data:image/png;base64, %s", alt="Login QR Code"/>
	</body>
	<script>
		async function subscribe() {
			let response = await fetch("/qr-callback?id=%s");
			if (response.status != 200) {
				console.log(response.statusText);
			} else {
				window.location.replace("/foobar?id=%s");
			}
		}
		subscribe();
	</script>
</html>`,
			imgBuf.String(), id.String(), id.String())))

		return
	} else {
		clientLog.Errorf("Login event: %s", evt.Event)
		w.WriteHeader(500)
		return
	}
}

func (s *WhatsappService) QrCallback(w http.ResponseWriter, req *http.Request) {
	callbackLog := waLog.Stdout("Client", "DEBUG", true)

	idStr := req.URL.Query().Get("id")
	if idStr == "" {
		callbackLog.Errorf("Login session id does not exist in query string")
		w.WriteHeader(500)
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		callbackLog.Errorf("Failed to parse session id: %v, %s", err, idStr)
		w.WriteHeader(500)
		return

	}
	qrChan, ok := s.idToQr.Load(id)
	if !ok {
		callbackLog.Errorf("Login session id %s does not exist in mapping", idStr)
		w.WriteHeader(500)
		return
	}

	evt := <-qrChan.(<-chan whatsmeow.QRChannelItem)
	callbackLog.Debugf("evt: %+v", evt)
	w.WriteHeader(200)
	return
}

func (s *WhatsappService) Start(w http.ResponseWriter, req *http.Request) {
	//startLog := waLog.Stdout("Start", "DEBUG", true)
	cookie := "abcd12345"

	query := url.Values{}
	query.Set("client_id", CLIENT_ID)
	query.Set("redirect_uri", REDIRECT_PATH+"/oauth/callback")
	query.Set("state", cookie)

	url := "https://auth.monday.com/oauth2/authorize?" + query.Encode()
	w.Header().Set("Set-Cookie", fmt.Sprintf("monday_auth_state=%s", cookie))
	w.Header().Set("Location", url)
	w.WriteHeader(302)
	return
}

func (s *WhatsappService) OAuthCallback(w http.ResponseWriter, req *http.Request) {
	oauthLog := waLog.Stdout("OAuth", "DEBUG", true)
	code := req.URL.Query().Get("code")
	//state := req.URL.Query().Get("state")
	//storedState, err := req.Cookie("monday_auth_state")
	//if err != nil {
	//	oauthLog.Errorf("Request did not have monday_auth_state cookie!")
	//	w.WriteHeader(400)
	//	return
	//}

	form := url.Values{}
	form.Add("redirect_uri", REDIRECT_PATH+"/oauth2/token")
	form.Add("client_id", CLIENT_ID)
	form.Add("client_secret", CLIENT_SECRET)
	form.Add("code", code)
	resp, err := http.PostForm("https://auth.monday.com/oauth2/token", form)
	if err != nil {
		oauthLog.Errorf("Monday auth returned error: %v", err)
		w.WriteHeader(500)
		return
	}

	bodyStr, err := io.ReadAll(resp.Body)
	if err != nil {
		oauthLog.Errorf("Failed to read Monday auth response body: %v", err)
		w.WriteHeader(500)
		return
	}

	oauthLog.Infof("CATCHME %s", bodyStr)
}

func main() {
	s, err := NewWhatsappService()
	if err != nil {
		panic(err)
	}

	public := http.NewServeMux()
	public.HandleFunc("/whatsapp-qr", s.SendWhatsappQR)
	public.HandleFunc("/qr-callback", s.QrCallback)
	public.HandleFunc("/start", s.Start)
	public.HandleFunc("/oauth/callback", s.OAuthCallback)

	http.ListenAndServe(":3000", public)
}
