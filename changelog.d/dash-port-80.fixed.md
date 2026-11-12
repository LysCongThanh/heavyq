**The dashboard works on port 80.** Browsers leave the default port out of `Host` and `Origin`, so every request to
`heavyq dash --addr 127.0.0.1:80` got 421 and the page never loaded. The bare loopback names are accepted on port
80; foreign names are still refused.
