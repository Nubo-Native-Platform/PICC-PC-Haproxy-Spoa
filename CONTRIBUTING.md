# Contributing to Nubo Native Platform (NNP)

This repository — **PICC - PC - HAProxy SPOA** — is part of the **Platform Infrastructure and Core
Components (PICC)** area of the Nubo Native Platform. Contributions are welcome
under the **Apache 2.0 License**.

## Before you start
Contribute against an open **Issue**, the published **Roadmap**, or a proposed
**enhancement**. Email **contribution@nubons.com** with your approach and
category first; we respond within 5 working days.

## Developer Certificate of Origin (DCO)
All contributions must adhere to the [Developer Certificate of Origin (DCO)](https://developercertificate.org/).
Every commit must be signed off by adding the following line to the commit message:

```
Signed-off-by: Random J Developer <random@developer.example.org>
```

This can be done automatically using the `git commit -s` command.

## Contribution Steps
1. Fork & clone the repository.
2. Create a focused topic branch (`git checkout -b feat/my-feature`).
3. Ensure all tests pass (`go test -v ./...`) and code passes static analysis (`go vet ./...`).
4. Commit your changes with a conventional commit message and DCO sign-off (`git commit -s -m "feat(spoa): add feature"`).
5. Open a Pull Request with a clear description and testing notes.

## Security
**Never commit secrets, tokens, `.env` files, or credentials.** See
[SECURITY.md](SECURITY.md). All participation is governed by our
[Code of Conduct](CODE_OF_CONDUCT.md).
