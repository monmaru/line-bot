package linebot

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"

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
	QueueName          = "default"
	TaskAnalyzeURL     = "/task/morphological-analysis"
	TaskUnsupportedURL = "/task/unsupported"
	Port               = ":8080"
	UserIDKey          = "mid"
	TextKey            = "text"
)

var dic tokenizer.Dic

func init() {
	err := godotenv.Load("line.env")
	if err != nil {
		panic(err)
	}

	dic = tokenizer.SysDic()
	http.HandleFunc(CallbackURL, handleCallback)
	http.HandleFunc(TaskAnalyzeURL, pushAnalysisResult)
	http.HandleFunc(TaskUnsupportedURL, pushUnsupportedMessage)
	http.HandleFunc("/", usage)
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

func handleCallback(w http.ResponseWriter, req *http.Request) {
	c := appengine.NewContext(req)
	bot, err := createBotClient(c)

	if err != nil {
		log.Criticalf(c, "linebot init error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	events, err := bot.ParseRequest(req)
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
				postUnsupportedTask(c, bot, event)
			case linebot.EventTypeBeacon:
				log.Debugf(c, "Got beacon!!")
				postUnsupportedTask(c, bot, event)
			default:
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
		postUnsupportedTask(c, bot, event)
	default:
		log.Debugf(c, "Got other foramt!!")
		postUnsupportedTask(c, bot, event)
	}
}

func postUnsupportedTask(c context.Context, bot *linebot.Client, event *linebot.Event) {
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
	message := tokenize(text) // 形態素解析

	pushTextMessage(c, bot, mid, message)
}

func pushUnsupportedMessage(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	bot, err := createBotClient(c)
	if err != nil {
		log.Criticalf(c, "linebot init error", err.Error())
		return
	}

	mid := r.FormValue(UserIDKey)
	message := "文字列以外はサポートしていません"

	pushTextMessage(c, bot, mid, message)
}

func pushTextMessage(c context.Context, bot *linebot.Client, userID string, message string) {
	log.Debugf(c, message)
	if _, err := bot.PushMessage(userID, linebot.NewTextMessage(message)).Do(); err != nil {
		log.Debugf(c, string(err.Error()))
	}
}

func tokenize(s string) (m string) {
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

func usage(w http.ResponseWriter, r *http.Request) {
	response := template.Must(template.ParseFiles("templates/usage.html"))
	response.Execute(w, struct {
		QR string
	}{
		QR: os.Getenv("QR_URL"),
	})
}
