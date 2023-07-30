A terminal based graphical monitor, for [Redis](https://redis.io/)!

## Installation

`go install github.com/qichengzx/rtop@latest`

### Building

```shell
git clone https://github.com/qichengzx/rtop.git
cd rtop
go build
./rtop
```

## Usage

Run `rtop -h` to see all available options. Current options are:

```
-addr string
    redis addr, ip:port (default "127.0.0.1:6379")
-password string
    redis auth password
```

## Built With

- [gizak/termui](https://github.com/gizak/termui)
- [go-redis/redis](https://github.com/go-redis/redis)
