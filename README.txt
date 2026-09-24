================================================================================
p2p-netcat-go-wrk — README
================================================================================
Рабочий форк santaklouse/go-p2p-netcat (github.com/santaklouse/go-p2p-netcat).
Репозиторий: https://github.com/sevoneceua/p2p-netcat-go-wrk
Лицензия: MIT (сохранена от апстрима, см. LICENSE)

Этот файл описывает ТЕКУЩЕЕ состояние проекта по состоянию на коммит d93d1f7
(Эпик 1, мультитуннель — базовая версия готова и проходит CI на Linux/
Windows/macOS). Структура: 1) что уже работает, 2) команды управления,
3) формат конфига мультитуннеля, 4) чего ещё нет, 5) сборка и CI.

================================================================================
1. ЧТО УЖЕ РАБОТАЕТ
================================================================================

1.1. Унаследовано от апстрима (без изменений)
--------------------------------------------------------------------------
  - Одиночный netcat-режим: `p2p-nc [options] [PeerId|multiaddr] [port]`
    (слушатель -l ИЛИ клиент, один процесс = одна пара "порт+режим")
  - Транспорты: TCP, QUIC, WebSocket/WSS, WebRTC (в т.ч. нативный
    WebRTC-сигналинг через Nostr/WebTorrent)
  - NAT traversal: DHT (Amino), mDNS, GossipSub-дискавери, Circuit Relay v2
  - Сессии: raw bridge, TCP/UDP forward, SOCKS4/4a/5 (сервер = exit-node),
    interactive PTY (сервер и клиент, есть Windows ConPTY), exec
  - Приватный pairing по токену (HKDF+AEAD), совместимый с исходным
    JS-клиентом на уровне протокола
  - Команды: `id` (показать PeerId), `token` (создать/расшифровать
    pairing-токен), `relay` (запустить relay-сервер)

1.2. Добавлено в этом форке (Эпик 1)
--------------------------------------------------------------------------
  [x] internal/tunnelconfig — YAML-схема мультитуннельного конфига
      (FRP-подобная модель: один конфиг, много туннелей, один процесс)
  [x] internal/daemon — супервизор: поднимает ОДИН p2p.Node и запускает
      на нём произвольное число туннелей одновременно (решает исходную
      проблему "один процесс = один порт")
  [x] p2p.Config.UpstreamSOCKS — узел может сам ходить наружу через
      внешний SOCKS5-прокси (не через torsocks-обёртку, работает и на
      Windows). При включении принудительно отключаются QUIC/WebRTC и
      обычный WebSocket-транспорт — они физически не могут идти через
      SOCKS5 в этой версии libp2p, и молчаливо это пропускать значило бы
      утечку мимо прокси. Остаётся TCP-транспорт (в т.ч. WSS можно
      поднять отдельно, если очень нужно — но по умолчанию выключен).
  [x] Команда `p2p-nc run --config <path>` — запускает мультитуннельный
      демон из YAML-файла

  [ ] Ещё не сделано (следующие шаги Эпика 1):
      - Hot-reload конфига (SIGHUP / file watcher) без перезапуска Node
      - Установка как служба (Windows SCM / systemd) — Эпик 2
      - Легаси-ветка под Windows Vista/7/8/Server 2008/2012 — отдельный трек
      - Native WebRTC гонка (race) для client-туннелей — сейчас клиент
        мультитуннеля использует только обычный OpenStream/OpenDatagramStream,
        без гонки с нативным WebRTC (в отличие от одиночного netcat-режима)

================================================================================
2. КОМАНДЫ УПРАВЛЕНИЯ
================================================================================

2.1. p2p-nc run — мультитуннельный демон (главное, что добавлено)
--------------------------------------------------------------------------
  p2p-nc run --config /path/to/tunnels.yaml [--quiet]

    -c, --config <path>   путь к YAML-конфигу туннелей (обязательный)
    -q, --quiet            подавить диагностику в stderr

  Поведение:
    - Валидирует конфиг (внутренняя логика, без обращения к сети)
    - Грузит/создаёт identity-ключ (cfg.identity, или identity.DefaultPath()
      если не указан — как и в одиночном режиме)
    - Поднимает один libp2p Host на весь процесс
    - Запускает каждый туннель из конфига; если хоть один не смог
      стартовать (например server-туннель не смог занять logical_port) —
      откатывает ВСЁ (и уже поднятые туннели, и сам Node) и завершается
      с ошибкой. Либо стартует весь конфиг целиком, либо ничего.
    - Печатает в stderr PeerId и адреса узла, затем список активных
      туннелей
    - Работает, пока не придёт Ctrl+C / SIGTERM — тогда все туннели и
      Node закрываются штатно

  Пример:
    $ p2p-nc run --config ~/.config/p2p-netcat/tunnels.yaml
    [p2p-ncd] PeerId: 12D3KooW...
    [p2p-ncd] address: /ip4/.../tcp/4001/p2p/12D3KooW...
    [p2p-ncd] 3 tunnel(s) running; Ctrl+C to stop
    [p2p-ncd] tunnel postgres-server: listening on logical port 15432 (forward/tcp)
    [p2p-ncd] tunnel exit-proxy: listening on logical port 10800 (socks/tcp)
    [p2p-ncd] tunnel postgres-client: local 127.0.0.1:15432 -> 12D3Koo... service 15432

2.2. Унаследованные команды (без изменений от апстрима)
--------------------------------------------------------------------------
  p2p-nc -l <port> [-d host] [-p port] [-S] [-e cmd] [-i] [-u]
      Одиночный слушатель. -d/-p — куда форвардить входящий поток,
      -S — SOCKS-сервер (exit-node), -e — выполнить команду, -i — PTY,
      -u — UDP вместо TCP.

  p2p-nc <PeerId|multiaddr> <port> [-p localport] [-i] [-u]
      Одиночный клиент. Без -p — просто пробрасывает stdin/stdout в
      поток (raw bridge). С -p — локальный форвард-листенер.

  p2p-nc id [-I identity-path]
      Показать PeerId для указанного (или дефолтного) identity-файла.

  p2p-nc token [logical-port] [--relay ...] [--expires-in N]
               [--encrypt-to path] [--password-file path]
      Создать приватный pairing-токен для конкретного PeerId+порта.
      См. `p2p-nc token --help` — там же расшифровка (token unlock).

  p2p-nc relay [опции]
      Запустить relay-сервер (Circuit Relay v2) — то, что вы будете
      разворачивать на своих VPS для устойчивости к блокировкам одного
      узла (см. обсуждение выше про публичные vs собственные relay).

  Общие флаги узла (--identity, --relay, --bootstrap, --ipv4/--ipv6,
  --no-dht, --no-mdns, --no-pubsub, --no-quic, --no-webrtc, --tor,
  --transport-port, --announce, --verbose) действуют так же, как в
  апстриме — см. `p2p-nc --help` для полного списка.

================================================================================
3. ФОРМАТ КОНФИГА (internal/tunnelconfig)
================================================================================

Пример полного конфига (postgres через forward, SOCKS5 exit-node,
удалённая admin-shell, выход в сеть через upstream SOCKS):

--------------------------------------------------------------------------
identity: ~/.config/p2p-netcat/identity.key

relays:
  - addr: /ip4/203.0.113.10/tcp/4001/p2p/12D3KooWRelayOne
    priority: 1
  - addr: /ip4/203.0.113.20/tcp/4001/p2p/12D3KooWRelayTwo
    priority: 2
fallback_to_public_dht: true      # держать opportunistic-discovery по
                                   # публичной DHT включённой параллельно
                                   # со своими relay (по умолчанию true)

upstream_socks: 127.0.0.1:9050    # весь исходящий TCP-трафик узла идёт
                                   # через этот SOCKS5-прокси (например Tor);
                                   # QUIC/WebRTC/WS при этом выключаются
                                   # автоматически, см. раздел 1.2

tunnels:
  # --- server: форвард входящего потока на локальный Postgres ---
  - name: postgres-server
    type: forward
    mode: server
    protocol: tcp
    logical_port: 15432            # p2p-адрес туннеля, НЕ сетевой порт
    target: 127.0.0.1:5432         # куда форвардить на этой машине
    token_file: ~/.config/p2p-netcat/tokens/postgres.token

  # --- client: тот же туннель с другой машины ---
  - name: postgres-client
    type: forward
    mode: client
    protocol: tcp
    logical_port: 15432
    peer: 12D3KooWExamplePeerID    # PeerId машины с postgres-server
    listen: 127.0.0.1:15432        # локальный bind:port для приложений
    token_file: ~/.config/p2p-netcat/tokens/postgres.token

  # --- server-only: SOCKS5 exit-node ---
  - name: exit-proxy
    type: socks
    mode: server
    logical_port: 10800
    token_file: ~/.config/p2p-netcat/tokens/socks.token

  # --- server-only: удалённая интерактивная shell ---
  - name: admin-shell
    type: pty
    mode: server
    logical_port: 2222
    token_file: ~/.config/p2p-netcat/tokens/shell.token
--------------------------------------------------------------------------

Поля Tunnel:
  name                  уникальное имя (для логов; не участвует в маршрутизации)
  type                  forward | socks | pty | exec
  mode                  server | client
  logical_port           1-65535, p2p-адрес туннеля (НЕ сетевой порт машины)
  protocol               tcp | udp — только для type: forward
  target                 host:port — куда форвардить (server + forward)
  listen                 bind:port — локальный листенер (client + forward)
  peer                   PeerId удалённой стороны (обязателен для client)
  exec                   команда для type: exec (только server)
  token_file             путь к pairing-токену (или allow_unauthenticated: true)
  allow_unauthenticated  явно разрешить туннель без токена (по умолчанию false)

Правила валидации (проверено против реального кода listenerlock/CLI):
  - socks, pty, exec — ТОЛЬКО server (зеркалит CLI: -S/-e/-i недоступны без -l)
  - logical_port уникален СРЕДИ SERVER-туннелей (у listenerlock лок только
    на стороне листенера — client с тем же logical_port на другого peer'а
    это не нарушает)
  - server-туннель privileged-типа (socks/pty/exec) требует ЛИБО token_file,
    ЛИБО явного allow_unauthenticated: true
  - upstream_socks — на уровне всего конфига, не на уровне туннеля (один
    процесс = один Host = один dialer)

================================================================================
4. ЧЕГО ЕЩЁ НЕТ (честно, чтобы не удивляться)
================================================================================
  - Hot-reload конфига на лету (сейчас — только полный рестарт процесса)
  - Установка как служба (Windows Service / systemd unit) — Эпик 2, не начат
  - Легаси-сборка под Windows Vista/7/8/Server 2008/2012 — отдельный трек,
    не начат (см. обсуждение в чате: Go 1.21+ физически не запускается на
    этих ОС, текущий go.mod требует 1.25.7 — нужен отдельный урезанный
    транспорт без libp2p)
  - Гонка с нативным WebRTC в client-туннелях мультитуннельного демона
    (в одиночном netcat-режиме она есть, здесь пока нет)
  - Не проверялось на Linux ARM / Android — отдельный шаг по плану

================================================================================
5. СБОРКА И CI
================================================================================
  Требуется Go >= 1.25.7 (см. go.mod).

    go build ./...
    go vet ./...
    go test ./...
    go test -race ./...

  В этом репозитории настроен GitHub Actions CI (.github/workflows/ci.yml,
  унаследован от апстрима): gofmt/vet, тесты на ubuntu-latest/windows-latest/
  macos-latest, race detector, сборка Docker-образа. Каждый пуш в main
  гоняет полный прогон — см. вкладку Actions в репозитории.

  Известное ограничение локальной разработки в песочнице без прямого
  доступа к proxy.golang.org: `go build`/`go test` там не работают "с нуля"
  (нет доступа к модульному прокси для скачивания зависимостей с чистого
  кеша). Реальная проверка сборки — через CI после пуша.
================================================================================
