package filobook_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	filobook "github.com/filobook/filobook-sdk/go"
)

// These examples compile with the tests but do not run (they have no Output comment), since they would call
// the live API. They are the same code as the README.

func Example_agent() {
	ctx := context.Background()
	fb := filobook.NewClient(filobook.WithAPIKey(os.Getenv("FILOBOOK_API_KEY")))

	order, err := fb.CreateOrder(ctx, filobook.CreateOrderInput{
		OrderInput: filobook.OrderInput{
			Request:  "Top 10 web results for x402 facilitators",
			Type:     filobook.TypeWebSearch,
			MaxPrice: "0.05",
		},
		QuoteWindowS: 60,
	})
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(10 * time.Second) // give providers time to quote

	detail, err := fb.Order(ctx, order.ID)
	if err != nil {
		log.Fatal(err)
	}
	for _, q := range detail.Quotes {
		price := "hidden (private)"
		if q.Price != nil {
			price = *q.Price
		}
		fmt.Println(q.ProviderName, price, q.Status)
	}

	fill, err := fb.FillOrder(ctx, order.ID)
	var apiErr *filobook.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "no_verified_quote" {
		fmt.Println("no verified quote yet")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("filled by", fill.ProviderName, "at", fill.Price, "saved", fill.Saved)
}

func Example_provider() {
	ctx := context.Background()
	fb := filobook.NewClient(filobook.WithAPIKey(os.Getenv("FILOBOOK_API_KEY")))

	me, err := fb.RegisterProvider(ctx, filobook.ProviderInput{
		Name:        "Example Search",
		EndpointURL: "https://search.example.com/x402",
		Types:       []filobook.RequestType{filobook.TypeWebSearch},
		WebhookURL:  filobook.String("https://search.example.com/filobook/webhook"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if me.WebhookSecret != nil {
		fmt.Println("store this webhook secret:", *me.WebhookSecret)
	}

	// Poll the feed alongside the webhook; webhook delivery is best effort.
	cursor := ""
	page, err := fb.Feed(ctx, filobook.FeedParams{Since: cursor})
	if err != nil {
		log.Fatal(err)
	}
	for _, o := range page.Orders {
		if _, err := fb.Quote(ctx, o.ID, filobook.QuoteInput{Price: "0.03", TTLS: 60}); err != nil {
			log.Println("quote", o.ID, err)
		}
	}
	if page.Cursor != nil {
		cursor = *page.Cursor
	}
	_ = cursor
}

func ExampleVerifyWebhookRequest() {
	secret := os.Getenv("FILOBOOK_WEBHOOK_SECRET")

	http.HandleFunc("/filobook/webhook", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		ev, err := filobook.VerifyWebhookRequest(r, secret)
		if err != nil {
			http.Error(w, "rejected", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		for _, o := range ev.Orders {
			log.Println("new order", o.ID, o.Type, "max", o.MaxPrice)
		}
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func ExampleClient_VerifyLedger() {
	ctx := context.Background()
	fb := filobook.NewClient()

	res, err := fb.VerifyLedger(ctx, filobook.VerifyLedgerOptions{
		OnProgress: func(h int64) { log.Println("verified up to height", h) },
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.OK {
		fmt.Println("chain intact: height", res.Height, "head", res.Head)
	} else {
		fmt.Println("broken at", res.BrokenAt, "reason", res.Reason)
	}
}

func ExampleParsePrice() {
	micro, ok := filobook.ParsePrice("0.0042")
	fmt.Println(micro, ok, filobook.FormatMicro(micro))
	_, ok = filobook.ParsePrice("1e-3")
	fmt.Println(ok)
	// Output:
	// 4200 true 0.0042
	// false
}
