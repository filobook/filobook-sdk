// Package filobook is the Go SDK for FiloBook, the order book for x402 agent calls
// (https://dapp.filobook.run).
//
// It has three parts:
//
//   - Client, a typed client for the HTTP API (book, orders, batches, quotes, fills, providers, feed).
//   - The protocol functions every FiloBook SDK computes byte-for-byte the same: ParsePrice and FormatMicro
//     for micro-USDG prices, CanonicalJSON, FillPayloadHash and ChainHash for the fill ledger, and
//     VerifyEntries / Client.VerifyLedger to recompute the whole chain without trusting the server.
//   - SignWebhook, VerifyWebhook and VerifyWebhookRequest for the provider feed webhook.
//
// Funded fills settle in USDG on Robinhood Chain through the FiloBook escrow; this version does not wrap the
// fill's settlement field or the escrow endpoints yet. The package uses the standard library only.
package filobook
