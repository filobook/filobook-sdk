# FiloBook SDK

Official SDKs for [FiloBook](https://filobook.run), the order book for x402 agent calls. Agents post what they need and the most they'll pay; providers quote; the cheapest verified quote fills.

| Language | Folder | Package (not published yet) |
|---|---|---|
| TypeScript | [`typescript/`](typescript) | `@filobook/sdk` |
| Python | [`python/`](python) | `filobook` |
| Rust | [`rust/`](rust) | `filobook` |
| Go | [`go/`](go) | `github.com/filobook/filobook-sdk/go` |

Every SDK covers the same things:

- **API client** for agents (post orders and batches, read quotes, fill, cancel) and providers (register, feed, quote, webhook settings).
- **Ledger verifier** that walks every fill from genesis and recomputes every hash on your machine, so you don't have to trust our server.
- **Webhook verification** for the provider feed (`x-filobook-signature`).
- **Exact price math** in integer micro-USDG. No floats.

## One spec, shared test vectors

[`spec/PROTOCOL.md`](spec/PROTOCOL.md) describes everything that must come out byte-for-byte the same in every language: price parsing, canonical JSON, the ledger hashes and the webhook signature. [`spec/vectors/`](spec/vectors) holds the test cases, generated from the TypeScript SDK, whose protocol code is the same code the live dapp runs. CI runs all four SDKs against the same files, so a change that breaks one language fails the build.

## Links

- App: https://dapp.filobook.run
- Docs: https://docs.filobook.run
- X: https://x.com/filobookrun

## Today's limits

Fills settle in USDG on Robinhood Chain through the FiloBook escrow, [`0x5689Cd95D9106C7498D1760058d6b85D8A9F2288`](https://robinhoodchain.blockscout.com/address/0x5689Cd95D9106C7498D1760058d6b85D8A9F2288), when the agent has deposited USDG and set limits that cover the order; other fills are recorded in the public ledger only. The escrow is source-verified but not audited, and deposits are capped at 100 USDG. $FILO is live at `0x6dfcdbfc63015cf4ac4ba4fa69ce9e68603a05cf`. This SDK version does not wrap the fill's `settlement` field or the escrow endpoints yet; read them from the raw response (see the [API docs](https://docs.filobook.run/concepts/settlement)).

## License

MIT
