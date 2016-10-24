package linebot

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ikawaha/kagome.ipadic/tokenizer"
	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/linebot"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"
	"google.golang.org/appengine/urlfetch"
)

// Constants
const (
	CallbackURL        = "/callback"
	TaskAnalyzeURL     = "/tasks/morphological-analysis"
	TaskUnsupportedURL = "/tasks/unsupported"
	Port               = ":8080"
	QueueName          = "default"
	UserIDKey          = "mid"
	TextKey            = "text"
)

var (
	dic     tokenizer.Dic
	initDic = new(sync.Once)
)

func init() {
	err := godotenv.Load("line.env")
	if err != nil {
		panic(err)
	}

	http.HandleFunc(CallbackURL, handleCallback)
	http.HandleFunc(TaskAnalyzeURL, pushAnalysisResult)
	http.HandleFunc(TaskUnsupportedURL, pushUnsupportedMessage)
	http.Handle("/", &templateHandler{
		filename: "usage.html",
		data: struct {
			QR string
		}{
			QR: os.Getenv("QR_URL"),
		}})
	http.ListenAndServe(Port, nil)
}

func createBotClient(c context.Context) (bot *linebot.Client, err error) {
	bot, err = linebot.New(
		os.Getenv("CHANNEL_SECRET"),
		os.Getenv("CHANNEL_TOKEN"),
		linebot.WithHTTPClient(urlfetch.Client(c)),
	)

	return
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	bot, err := createBotClient(c)

	if err != nil {
		log.Criticalf(c, "linebot init error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	events, err := bot.ParseRequest(r)
	if err != nil {
		if err == linebot.ErrInvalidSignature {
			log.Errorf(c, "Invalid signature")
			w.WriteHeader(http.StatusBadRequest)
		} else {
			log.Errorf(c, "Error on parse request")
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	// とりあえずタスクキューにつっこんですぐにレスポンスを返す
	for _, event := range events {
		if event.Source.Type == linebot.EventSourceTypeUser {
			switch event.Type {
			case linebot.EventTypeMessage:
				handleMessageEvent(c, bot, event)
			case linebot.EventTypePostback:
				log.Debugf(c, "Got postBack!!")
				addUnsupportedTask(c, bot, event)
			case linebot.EventTypeBeacon:
				log.Debugf(c, "Got beacon!!")
				addUnsupportedTask(c, bot, event)
			default:
				log.Debugf(c, "Got other event!!")
				addUnsupportedTask(c, bot, event)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

func handleMessageEvent(c context.Context, bot *linebot.Client, event *linebot.Event) {
	source := event.Source
	switch message := event.Message.(type) {
	case *linebot.TextMessage:
		log.Debugf(c, "Got text!!")
		task := taskqueue.NewPOSTTask(TaskAnalyzeURL, url.Values{
			UserIDKey: {source.UserID},
			TextKey:   {message.Text},
		})
		taskqueue.Add(c, task, QueueName)
	case *linebot.ImageMessage:
		log.Debugf(c, "Got image!!")
		addUnsupportedTask(c, bot, event)
	default:
		log.Debugf(c, "Got other foramt!!")
		addUnsupportedTask(c, bot, event)
	}
}

func addUnsupportedTask(c context.Context, bot *linebot.Client, event *linebot.Event) {
	source := event.Source
	task := taskqueue.NewPOSTTask(TaskUnsupportedURL, url.Values{
		UserIDKey: {source.UserID},
	})
	taskqueue.Add(c, task, QueueName)
}

func pushAnalysisResult(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	bot, err := createBotClient(c)
	if err != nil {
		log.Criticalf(c, "linebot init error", err.Error())
		return
	}

	mid := r.FormValue(UserIDKey)
	text := r.FormValue(TextKey)

	pushText(c, bot, mid, tokenize(text))
}

func pushUnsupportedMessage(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	bot, err := createBotClient(c)
	if err != nil {
		log.Criticalf(c, "linebot init error", err.Error())
		return
	}

	mid := r.FormValue(UserIDKey)
	pushText(c, bot, mid, "文字列以外はサポートしていません")
}

func pushText(c context.Context, bot *linebot.Client, userID string, message string) {
	log.Debugf(c, message)
	if _, err := bot.PushMessage(userID, linebot.NewTextMessage(message)).Do(); err != nil {
		log.Debugf(c, string(err.Error()))
	}
}

func tokenize(s string) (m string) {
	// 辞書の初期化は、必要になった時点で一度だけ行う。
	initDic.Do(func() {
		dic = tokenizer.SysDic()
	})
	t := tokenizer.NewWithDic(dic)
	tokens := t.Tokenize(s)
	for _, token := range tokens {
		if token.Class == tokenizer.DUMMY {
			continue
		}
		features := strings.Join(token.Features(), ",")
		m += token.Surface + "    " + features + "\n"
	}
	return
}

type templateHandler struct {
	once     sync.Once
	filename string
	templ    *template.Template
	data     interface{}
}

func (t *templateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("cache-control", "public, max-age=86400") // CDNにキャッシュさせる
	// テンプレートのコンパイルが一度で済むようにsync.Once型を使う。
	t.once.Do(func() {
		t.templ = template.Must(template.ParseFiles(filepath.Join("templates", t.filename)))
	})
	t.templ.Execute(w, t.data)
}
