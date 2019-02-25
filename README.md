cas
===

> Command as a Service.

Allows you to execute commands in your server via HTTP. Commands to execute are defined in YAML config file.

[![Build][travis-image]][travis-url]

## Usage

Given `config.yml`:

```yaml
/hello_world:
    command: echo
    args:
        - hello world
/ping/google:
    command: ping
    args:
        - -c
        - 1
        - google.com
```

start `cas` with:

```
cas -c config.yml
```

Defined commands can be requested via POST:

```
curl -X POST 'http://localhost:1307/hello_world'

HTTP/1/1 200 OK
{
  "request_id": "bhpfi1krtr32369i8keg",
  "output": "hello world\n",
  "error": "",
  "status": 200
}

curl -X POST 'http://localhost:1307/ping/google'

HTTP/1/1 200 OK
{
  "request_id": "bhpfifkrtr32369i8kfg",
  "output": "PING google.com (216.239.38.120): 56 data bytes\n64 bytes from 216.239.38.120: icmp_seq=0 ttl=52 time=29.604 ms\n\n--- google.com ping statistics ---\n1 packets transmitted, 1 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 29.604/29.604/29.604/0.000 ms\n",
  "error": "",
  "status": 200
}
```

Successful request always responded with 200 OK, even if command exited with
non-zero.

### Passing args

**config.yml:**

```yaml
/hello:
    command: echo
    args:
        - hello
    allow:
        - args
```

**example request:**

```
curl -X POST 'http://localhost:1307/hello' \
    -H 'Content-Type: application/json' \
    -d '{"args": ["world", "my dear"]}'

HTTP/1/1 200 OK
{
  "request_id": "bhpfjokrtr323oi3qqm0",
  "output": "hello world my dear\n",
  "error": "",
  "status": 200
}
```

If `args` is not in `allow` list, then http 403 is returned:

```
HTTP/1.1 403 Forbidden
{
  "request_id": "bhpfkocrtr32413hipt0",
  "output": "",
  "error": "args param is not allowed",
  "status": 403
}
```

### Passing envs

**config.yml:**

```yaml
/env:
    command: env
    envs:
        - FOO=BAR
        - BAR=BAZ
    allow:
        - envs
```

**example request:**

```
curl -X POST 'http://localhost:1307/env' \
    -H 'Content-Type: application/json' \
    -d '{"envs": ["SOMETHING=ELSE"]}'

HTTP/1/1 200 OK
{
  "request_id": "bhpfmfcrtr324eph7e40",
  "output": "FOO=BAR\nBAR=BAZ\nSOMETHING=ELSE\n",
  "error": "",
  "status": 200
}
```

If `envs` is not in `allow` list, then http 403 is returned:

```
HTTP/1.1 403 Forbidden
{
  "request_id": "bhpfonkrtr324l2p6pgg",
  "output": "",
  "error": "envs param is not allowed",
  "status": 403
}
```

### Passing stdin

**./file.txt:**

```
hello
there
```

**config.yml:**

```yaml
/diff:
   command: diff
   args:
       - file.txt
       - '-'
   dir: ./ 
   allow:
       - stdin
```

**example request:**

```
curl -X POST 'http://localhost:1307/diff' \
    -H 'Content-Type: application/json' \
    -d '{"stdin": "hello"}'

HTTP/1/1 200 OK
{
  "request_id": "bhpfso4rtr324ri6polg",
  "output": "1,2c1\n< hello\n< there\n---\n> hello\n\\ No newline at end of file\n",
  "error": "exit status 1",
  "status": 200
}
```

If `stdin` is not in `allow` list, then http 403 is returned:

```
HTTP/1.1 403 Bad Request
{
  "request_id": "bhpftgcrtr3254845s9g",
  "output": "",
  "error": "stdin param is not allowed",
  "status": 403
}
```

### Callback / Webhook

**config.yml:**

```yaml
/ping:
    command: ping
    args:
        - -c
        - 3
        - -i
        - 2
        - gogle.com
    allow:
        - callback

```

**example request:**

```
curl -X POST 'http://localhost:1307/ping' \
    -H 'Content-Type: application/json' \
    -d '{"callback": "https://postb.in/DPb3wkdG"}'
{
  "request_id": "bhpfso4rtr324ri6polg",
  "url": "https://postb.in/DPb3wkdG",
}
```

`cas` will post the result to https://postb.in/DPb3wkdG:

```
{
  "request_id": "bhpfso4rtr324ri6polg",
  "output": "PING gogle.com (172.217.194.103): 56 data bytes\n64 bytes from 172.217.194.103: icmp_seq=0 ttl=42 time=29.158 ms\n64 bytes from 172.217.194.103: icmp_seq=1 ttl=42 time=24.744 ms\n64 bytes from 172.217.194.103: icmp_seq=2 ttl=42 time=25.163 ms\n\n--- gogle.com ping statistics ---\n3 packets transmitted, 3 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 24.744/26.355/29.158/1.989 ms\n",
  "error": "",
  "status": 200
}

```

[travis-image]: https://img.shields.io/travis/gedex/cas/master.svg?label=linux
[travis-url]: https://travis-ci.org/gedex/cas
