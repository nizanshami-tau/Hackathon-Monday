package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

// const DB_PATH string = "/var/local/whatsapp-broker/app.db"
const DB_PATH string = "/tmp/myapp.db"
const SVC_PREFIX string = "/gosvc"
const REDIRECT_PATH string = "https://sunday.sviry.net" + SVC_PREFIX
const CLIENT_ID string = "63096c5c98c7077e0a8db84a4a21b299"
const CLIENT_SECRET string = "90d0e45f578ec3d092c4806674dd4033"

type User struct {
	AccessToken       string
	WSClient          *whatsmeow.Client
	Conversations     []*proto.Conversation
	ConversationsLock sync.Mutex
}

type WhatsappService struct {
	container     *sqlstore.Container
	logger        waLog.Logger
	idToQr        sync.Map
	sessionToUser sync.Map
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

	sessionCookie, err := req.Cookie("sessionid")
	if err != nil {
		clientLog.Errorf("Failed to get request session id cookie:", err)
		w.WriteHeader(400)
		return
	}

	sessionID := sessionCookie.Value
	user, ok := s.sessionToUser.Load(uuid.MustParse(sessionID))
	if !ok {
		clientLog.Errorf("Failed to find sessionid in mapping")
		w.WriteHeader(400)
		return
	}

	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(func(evt interface{}) {
		client.Log.Infof("CATCHME 4 %v", reflect.TypeOf(evt))
		if hs, ok := evt.(*events.HistorySync); ok {
			u := user.(*User)
			u.ConversationsLock.Lock()
			defer u.ConversationsLock.Unlock()
			u.Conversations = append(u.Conversations, hs.Data.Conversations...)
			client.Log.Infof("CATCHME 3 saved %d conversations", len(hs.Data.Conversations))
		}
	})

	qrCtx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	qrChan, _ := client.GetQRChannel(qrCtx)
	err = client.Connect()
	if err != nil {
		clientLog.Errorf("Connect error: %s", err)
		w.WriteHeader(500)
		return
	}

	user.(*User).WSClient = client

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
			panic(err)
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
			let response = await fetch("/gosvc/qr-callback?id=%s");
			if (response.status != 200) {
				console.log(response.statusText);
			} else {
				window.location.replace("/");
			}
		}
		subscribe();
	</script>
</html>`,
			imgBuf.String(), id.String())))

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
	sessionCookie, err := req.Cookie("sessionid")
	if err != nil {
		callbackLog.Errorf("Failed to get request session id cookie:", err)
		w.WriteHeader(400)
		return
	}

	sessionID := sessionCookie.Value
	user, ok := s.sessionToUser.Load(uuid.MustParse(sessionID))
	if !ok {
		callbackLog.Errorf("Failed to find sessionid in mapping")
		w.WriteHeader(400)
		return
	}

	client := user.(*User).WSClient
	for _, name := range appstate.AllPatchNames {
		err := client.FetchAppState(name, true, false)
		if err != nil {
			client.Log.Errorf("Failed to do initial fetch of app state %s: %v", name, err)
		}
	}

	callbackLog.Debugf("evt: %+v", evt)
	w.WriteHeader(200)
	return
}

func (s *WhatsappService) Start(w http.ResponseWriter, req *http.Request) {
	startLog := waLog.Stdout("Start", "DEBUG", true)
	cookie := "abcd12345"

	query := url.Values{}
	query.Set("client_id", CLIENT_ID)
	query.Set("redirect_uri", REDIRECT_PATH+"/oauth/callback")
	query.Set("state", cookie)

	startLog.Infof("CATCHME 1 %+v", query)

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
	form.Add("redirect_uri", REDIRECT_PATH+"/oauth/callback")
	form.Add("client_id", CLIENT_ID)
	form.Add("client_secret", CLIENT_SECRET)
	form.Add("code", code)
	oauthLog.Infof("CATCHME 2 %+v", form)
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

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}

	err = json.Unmarshal(bodyStr, &body)
	if err != nil {
		oauthLog.Errorf("Unmarshal monday json response error: %v", err)
		w.WriteHeader(500)
		return
	}

	oauthLog.Errorf("CATCHE 6: %+v", body)

	query := url.Values{}
	query.Set("status", "success")
	query.Set("access_token", body.AccessToken)
	query.Set("refresh_token", body.RefreshToken)
	query.Set("token_type", body.TokenType)
	query.Set("scope", body.Scope)

	sessionID, err := uuid.NewRandom()
	if err != nil {
		panic(err)
	}

	s.sessionToUser.LoadOrStore(sessionID, &User{
		AccessToken: body.AccessToken,
	})

	w.Header().Set("Set-Cookie", fmt.Sprintf("sessionid=%s; path=/", sessionID.String()))
	url := REDIRECT_PATH + "/whatsapp-qr?" //+ query.Encode()
	w.Header().Set("Location", url)
	w.WriteHeader(302)
}

type GroupOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s *WhatsappService) ListGroups(w http.ResponseWriter, req *http.Request) {
	listGroupsLog := waLog.Stdout("ListGroups", "DEBUG", true)
	sessionCookie, err := req.Cookie("sessionid")
	if err != nil {
		listGroupsLog.Errorf("Failed to get request session id cookie:", err)
		w.WriteHeader(400)
		return
	}

	sessionID := sessionCookie.Value
	user, ok := s.sessionToUser.Load(uuid.MustParse(sessionID))
	if !ok {
		listGroupsLog.Errorf("Failed to find sessionid in mapping")
		w.WriteHeader(400)
		return
	}

	client := user.(*User).WSClient
	groups, err := client.GetJoinedGroups()
	if err != nil {
		listGroupsLog.Errorf("Failed to fetch groups: %v", err)
		w.WriteHeader(500)
		return
	}

	options := make([]GroupOption, len(groups))
	for i, group := range groups {
		options[i] = GroupOption{
			Label: group.Name,
			Value: group.Topic,
		}
	}

	groupsJSON, err := json.Marshal(options)
	if err != nil {
		listGroupsLog.Errorf("Failed to fetch groups: %v", err)
		w.WriteHeader(500)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(groupsJSON)
}

type Data struct {
	Data CreateBoard `json:"data"`
}

type CreateBoard struct {
	CreateBoard GroupID `json:"create_board"`
}

type GroupID struct {
	Id string `json:"id"`
}

func (s *WhatsappService) ChooseGroups(w http.ResponseWriter, req *http.Request) {
	chooseGroupsLog := waLog.Stdout("ChooseGroups", "DEBUG", true)
	sessionCookie, err := req.Cookie("sessionid")
	if err != nil {
		chooseGroupsLog.Errorf("Failed to get request session id cookie:", err)
		w.WriteHeader(400)
		return
	}

	sessionID := sessionCookie.Value
	user, ok := s.sessionToUser.Load(uuid.MustParse(sessionID))
	if !ok {
		chooseGroupsLog.Errorf("Failed to find sessionid in mapping")
		w.WriteHeader(400)
		return
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		chooseGroupsLog.Errorf("Failed to read request body")
		w.WriteHeader(500)
		return
	}

	var groups []GroupOption
	err = json.Unmarshal(bodyBytes, &groups)
	if err != nil {
		chooseGroupsLog.Errorf("Failed to unmarshal request body: %v %s", err, string(bodyBytes))
		w.WriteHeader(500)
		return
	}

	msgArr := make([]*proto.HistorySyncMsg, 0)
	userObj := user.(*User)
	userObj.ConversationsLock.Lock()
	defer userObj.ConversationsLock.Unlock()
	for _, c := range userObj.Conversations {
		for _, g := range groups {
			if c.Name != nil && *c.Name == g.Label {
				for _, m := range c.Messages {
					msgArr = append(msgArr, m)
				}
			}
		}
	}

	go func() {
		for _, g := range groups {
			query, err := json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation {
    create_board (board_name: "%s", board_kind: public) {
        id
    }
}
`, g.Label),
			})
			userObj.WSClient.Log.Errorf("CATCHME 98 %+v", string(query))
			if err != nil {
				panic(err)
			}
			req, err := http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			userObj.WSClient.Log.Errorf("CATCHME 99 %+v %+v", resp, string(bodyBytes))

			var result Data
			err = json.Unmarshal(bodyBytes, &result)
			if err != nil {
				panic(err)
			}

			query, err = json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation {
    create_group (board_id: %s, group_name: "Exercises") {
        id
    }
}
`, result.Data.CreateBoard.Id),
			})
			req, err = http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}

			bodyBytes, err = io.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			userObj.WSClient.Log.Errorf("CATCHME 99 %+v %+v", resp, string(bodyBytes))

			query, err = json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation {
    create_group (board_id: %s, group_name: "Tests") {
        id
    }
}
`, result.Data.CreateBoard.Id),
			})
			req, err = http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}

			bodyBytes, err = io.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			userObj.WSClient.Log.Errorf("CATCHME 99 %+v %+v", resp, string(bodyBytes))

			query, err = json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation {
    create_group (board_id: %s, group_name: "Tirgulim") {
        id
    }
}
`, result.Data.CreateBoard.Id),
			})
			req, err = http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}
			bodyBytes, err = io.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			userObj.WSClient.Log.Errorf("CATCHME 99 %+v %+v", resp, string(bodyBytes))

			query, err = json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation {
    create_group (board_id: %s, group_name: "Lectures") {
        id
    }
}
`, result.Data.CreateBoard.Id),
			})
			req, err = http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}

			query, err = json.Marshal(struct {
				Query string `json:"query"`
			}{
				Query: fmt.Sprintf(`
mutation{
  create_column(board_id: %s, title:"Files", description: "files", column_type:file) {
    id
    title
    description 
  }
}
`, result.Data.CreateBoard.Id),
			})
			req, err = http.NewRequest("POST", "https://api.monday.com/v2", bytes.NewReader(query))
			if err != nil {
				panic(err)
			}

			req.Header.Set("Authorization", userObj.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Length", fmt.Sprintf("%d", len(query)))

			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				panic(err)
			}

			for _, m := range msgArr {
				data, err := userObj.WSClient.DownloadAny(m.Message.Message)
				if err == nil {
					f, err := ioutil.TempFile(".", "foobar13")
					if err != nil {
						panic(err)
					}
					err = ioutil.WriteFile(f.Name(), data, 0600)
					if err != nil {
						panic(err)
					}
					chooseGroupsLog.Errorf("CATCHME 102 %+v, %+v", m.Message.MediaData.GetLocalPath(), filepath.Base(m.Message.MediaData.GetLocalPath()))
					fname := filepath.Base(m.Message.MediaData.GetLocalPath())
					groupID := "exercises"
					if strings.Contains(fname, "sol") {
						groupID = "exercises"
					} else if m.Message.Message.ImageMessage != nil {
						groupID = "tests"
					} else if strings.Contains(fname, "2020") {
						groupID = "tests"
					} else if strings.Contains(fname, "practice") {
						groupID = "tirgul"
					}
					chooseGroupsLog.Infof("CATCHME 5 %+v, %+v", data, groupID)
					mycmd := fmt.Sprintf("python3 ../monday_files.py --path \"%s\" --file \"%s\" --board_id \"%s\", --group_id \"%s\"", f.Name(), fname, result.Data.CreateBoard.Id, groupID)
					userObj.WSClient.Log.Errorf("CATCHME 103 %+v", mycmd)
					cmd := exec.Command("bash", "-c", mycmd)
					ob := bytes.Buffer{}
					eb := bytes.Buffer{}
					cmd.Stdout = &ob
					cmd.Stderr = &eb
					err := cmd.Run()
					if err != nil {
						userObj.WSClient.Log.Errorf("CATCHME 102 %+v %+v %+v", err, ob.String(), eb.String())
					}
				}
			}
		}
	}()
}

func main() {
	s, err := NewWhatsappService()
	if err != nil {
		panic(err)
	}

	public := http.NewServeMux()
	public.HandleFunc(SVC_PREFIX+"/whatsapp-qr", s.SendWhatsappQR)
	public.HandleFunc(SVC_PREFIX+"/qr-callback", s.QrCallback)
	public.HandleFunc(SVC_PREFIX+"/start", s.Start)
	public.HandleFunc(SVC_PREFIX+"/oauth/callback", s.OAuthCallback)
	public.HandleFunc(SVC_PREFIX+"/listgroups", s.ListGroups)
	public.HandleFunc(SVC_PREFIX+"/choosegroup", s.ChooseGroups)

	http.ListenAndServe(":3000", public)
}
