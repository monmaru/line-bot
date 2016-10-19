package linebot

import (
	"net/http"
	"os"
	"strings"

	"github.com/ikawaha/kagome.ipadic/tokenizer"
	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/linebot"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

// Constants
const (
	CallbackURL = "/callback"
	Port        = ":8080"
)

var dic tokenizer.Dic

func init() {
	err := godotenv.Load("line.env")
	if err != nil {
		panic(err)
	}

	dic = tokenizer.SysDic()
	http.HandleFunc(CallbackURL, handleCallback)
	http.ListenAndServe(Port, nil)
}

func handleCallback(w http.ResponseWriter, req *http.Request) {
	c := appengine.NewContext(req)

	bot, err := linebot.New(
		os.Getenv("CHANNEL_SECRET"),
		os.Getenv("CHANNEL_TOKEN"),
		linebot.WithHTTPClient(urlfetch.Client(c)),
	)

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

	for _, event := range events {
		switch event.Type {
		case linebot.EventTypeMessage:
			messageHandler(c, bot, event)
		case linebot.EventTypePostback:
			postbackHanlder(c, bot, event)
		case linebot.EventTypeBeacon:
			beaconHandler(c, bot, event)
		default:
		}
	}
	w.WriteHeader(http.StatusOK)
}

func messageHandler(c context.Context, bot *linebot.Client, event *linebot.Event) {
	switch message := event.Message.(type) {
	case *linebot.TextMessage:
		pushTextMessage(c, bot, event, tokenize(message.Text))
	case *linebot.ImageMessage:
		pushTextMessage(c, bot, event, "Got image!!")
	default:
		pushTextMessage(c, bot, event, "Got message!!")
	}
}

func postbackHanlder(c context.Context, bot *linebot.Client, event *linebot.Event) {
	pushTextMessage(c, bot, event, "Got PostBack!!")
}

func beaconHandler(c context.Context, bot *linebot.Client, event *linebot.Event) {
	pushTextMessage(c, bot, event, "Got beacon!!")
}

func pushTextMessage(c context.Context, bot *linebot.Client, event *linebot.Event, message string) {
	source := event.Source
	if source.Type == linebot.EventSourceTypeUser {
		log.Debugf(c, message)
		if _, err := bot.PushMessage(source.UserID, linebot.NewTextMessage(message)).Do(); err != nil {
			log.Debugf(c, string(err.Error()))
		}
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
