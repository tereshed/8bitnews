# Деплой hub (E1d)

Разворачивает hub на сервере владельца (Hetzner, Хельсинки, `135.181.4.110`), который уже
обслуживает другой проект (Spanish Tutor). hub работает внутри изолированной KVM VM: взлом hub
не должен дать доступ к соседнему проекту и его данным. Всё в этом документе выполняется с
машины-контроллера (ноутбук оператора или CI-раннер с SSH-доступом на сервер) — не на самом
сервере и не из песочницы агента.

Playbooks и Make-цели: `deploy/ansible/`, см. также `deploy/ansible/deploy.env.example`.

## 0. Что нужно сделать руками один раз, до первого запуска

1. **Домен.** `hub.8bitlang.org` уже заведён: A-запись на `135.181.4.110` в Cloudflare DNS
   существует и уже в режиме **«DNS only»** (серое облако, не оранжевое) — это важно: proxy
   Cloudflare перед Caddy сломает TLS-рукопожатие Caddy с letsencrypt/ZeroSSL. Этот домен —
   соседский (hub отдаёт только API для агентов, отдельный поддомен ему не нужен), ничего заводить
   не требуется — только убедитесь, что режим прокси остался «DNS only», и используйте
   `HUB_DOMAIN=hub.8bitlang.org` (это уже значение по умолчанию в плейбуках, см. п.7).
2. **Два бакета Cloudflare R2**, оба с юрисдикцией **EU**:
   - бакет для медиа релизов — `8bitnews-media`;
   - бакет для бэкапов Postgres — `8bitnews-backups`, отдельный от медиа.
   У бакетов с юрисдикцией EU S3-эндпоинт содержит сегмент `.eu.`:
   `https://<account_id>.eu.r2.cloudflarestorage.com` (без `.eu.` — это не тот же бакет).
3. **Один R2 API-токен**, выданный только на эти два бакета (Object Read & Write), не на весь
   аккаунт. Cloudflare даёт единую пару Access Key ID/Secret Access Key для S3-совместимого API —
   это `R2_ACCESS_KEY_ID`/`R2_SECRET_ACCESS_KEY`, общие для обоих бакетов (не два разных токена).
   Регион для R2 всегда `auto`.
4. **Пароль restic** — сгенерируйте случайную строку (например, `openssl rand -base64 32`) и
   положите в менеджер паролей. Без него бэкапы в R2 невосстановимы — это не секрет для git и не
   секрет для сервера в обычном смысле, это единственный ключ к шифрованию бэкапов.
5. **SSH-ключ оператора** (публичный) — тот, которым вы будете заходить в гостевую VM.
6. Заполните `~/.config/8bitnews/deploy.env` (0600) по образцу `deploy/ansible/deploy.env.example`:
   `HUB_ADMIN_SSH_PUBLIC_KEY`, `R2_ENDPOINT` (с `.eu.`), `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`,
   `R2_MEDIA_BUCKET`, `R2_BACKUP_BUCKET`, `RESTIC_PASSWORD`. `HUB_DOMAIN` можно не трогать — по
   умолчанию уже `hub.8bitlang.org`. Repository URL для restic (`s3:<R2_ENDPOINT>/<R2_BACKUP_BUCKET>`)
   плейбук собирает сам — отдельно его задавать не нужно. Этот файл не попадает в git.
7. На машине-контроллере должны быть: `ansible-core` (`pip install ansible-core` или системный
   пакет) и коллекции из `deploy/ansible/requirements.yml`:
   ```sh
   ansible-galaxy collection install -r deploy/ansible/requirements.yml
   ```
8. SSH-доступ root на `135.181.4.110` должен уже работать (ключ оператора в `/root/.ssh/authorized_keys`
   на хосте — это делает инфраструктура соседа, не этот плейбук). Каталог `/etc/caddy/sites/` и
   строка `import /etc/caddy/sites/*.caddy` в `/etc/caddy/Caddyfile` соседа уже на месте.

## 1. Первый деплой

```sh
make deploy-hub-vm       # хост: VM, сетевая изоляция, сайт Caddy
make deploy-hub          # гость: Docker, checkout main, migrate → sync-profiles → serve → healthz
make deploy-hub-backup   # гость: ежедневные бэкапы в R2 + restore-check
```

`deploy-hub-vm` создаёт VM `8bitnews-hub` (2 vCPU, 4096 МБ, диск 60 ГБ) под отдельным системным
пользователем в группе `kvm`, с nft-таблицей, блокирующей для этого пользователя соединения на
loopback/приватные сети хоста и публичный IP хоста (кроме DNS), и кладёт
`/etc/caddy/sites/8bitnews.caddy` — **только если** в `/etc/caddy/Caddyfile` уже есть
`import /etc/caddy/sites/*.caddy`; если импорта нет, плейбук останавливается и ничего не меняет
(добавление импорта — задача Ansible соседа, не этого плейбука).

`deploy-hub` ждёт, что VM из шага выше уже поднята и отвечает по SSH через хост (см. ниже). Она
устанавливает Docker, выкатывает `github.com/tereshed/8bitnews` на ref из `HUB_REF`
(по умолчанию `main`; `make deploy-hub REF=<ref>` — для конкретного релиза/тега/коммита), один раз
генерирует пароль Postgres и `HUB_API_KEYS` (дальше не трогает их), и на каждом запуске прогоняет
`migrate up` → `sync-profiles` → пересобирает и перезапускает `serve` → ждёт `/healthz`.

`deploy-hub-backup` ставит `restic`, инициализирует репозиторий (если ещё не инициализирован) и
включает systemd-таймер на ежедневный `pg_dump` + отправку в R2-бакет бэкапов.

## 2. Как получить HUB_API_KEY для PolyMaths

`HUB_API_KEYS` генерируется один раз на госте при первом запуске `make deploy-hub` и не меняется
последующими запусками. Чтобы его прочитать:

```sh
ssh -J root@135.181.4.110 bitnews@127.0.0.1 -p 2223 "sudo grep HUB_API_KEYS /opt/8bitnews/deploy/.env"
```

(`bitnews` — значение `hub_admin_user`; `-p 2223` — порт, на котором хост форвардит SSH в гостя,
`hub_vm_ssh_port`). Это значение — `${secret:HUB_API_KEY}` в заголовке MCP-подключения PolyMaths
к hub (`docs/architecture.md` §5): `Authorization: Bearer <значение>`. hub принимает несколько
ключей через запятую в `HUB_API_KEYS`, так что ключ для PolyMaths можно выпустить отдельно от
любого ключа для ручных проверок — для этого отредактируйте `/opt/8bitnews/deploy/.env` на госте
вручную и перезапустите `hub`: `docker compose --project-directory /opt/8bitnews/deploy up -d --force-recreate hub`.
Эта правка — ручная операция на госте, `make deploy-hub` её не трогает (он только читает
существующие `POSTGRES_PASSWORD`/`HUB_API_KEYS` обратно, не перезаписывает).

## 3. Прямой доступ в гостя (отладка)

Гость не слушает ни одного порта снаружи — только через хост:

```sh
ssh -J root@135.181.4.110 bitnews@127.0.0.1 -p 2223
```

Хост-ключ гостя сгенерирован `hub_vm.yml` на самой машине-хосте и уже добавлен в known_hosts
контроллера, который выполнял `make deploy-hub-vm` — при первом подключении другого контроллера
(другого ноутбука/раннера) `ssh` предупредит о неизвестном ключе хопа `127.0.0.1:2223`; это
ожидаемо, если этот конкретный контроллер ещё не запускал `deploy-hub-vm`.

## 4. Обновление hub

```sh
make deploy-hub REF=<новый ref>
```

Это пересобирает образ из обновлённого checkout, прогоняет миграции и перезапускает `serve`;
Postgres и его том не трогаются. `channels/`/`countries/` синхронизируются из того же checkout на
каждом запуске (`sync-profiles` идемпотентен).

## 5. Откат

```sh
make deploy-hub REF=<предыдущий ref-или-тег-или-коммит>
```

Откат — это деплой старого ref тем же путём. Если откат нужен из-за новой миграции, которая
ломает старый код: `goose`-миграции в `migrations/` обратимы (`hub migrate down` внутри гостя),
но откатывать схему вручную стоит только когда вы уверены, что между версиями не было
несовместимых изменений данных — в общем случае откатывайте код, не схему.

## 6. Восстановление базы из бэкапа

Проверка восстановления (не трогает живую базу — поднимает отдельный изолированный Postgres без
сети):

```sh
ssh -J root@135.181.4.110 bitnews@127.0.0.1 -p 2223 "sudo /opt/8bitnews/backup/restore-check.sh"
```

Восстановление **в живую** базу (ручная операция, делать осознанно):

```sh
ssh -J root@135.181.4.110 bitnews@127.0.0.1 -p 2223
sudo bash
set -a; source /etc/8bitnews-backup.env; set +a
restic restore latest --tag 8bitnews-hub --target /tmp/restore
docker compose --project-directory /opt/8bitnews/deploy stop hub
docker compose --project-directory /opt/8bitnews/deploy exec -T postgres \
  pg_restore -U 8bitnews -d 8bitnews --clean --if-exists /tmp/restore/var/backups/8bitnews/<файл>.pgdump
docker compose --project-directory /opt/8bitnews/deploy up -d hub
```

(путь `/tmp/restore/var/backups/8bitnews/...` — restic восстанавливает по исходному абсолютному
пути дампа; `ls /tmp/restore/var/backups/8bitnews/` покажет точное имя файла).

## 7. Остановка и удаление VM — без вреда соседу

Остановить:

```sh
ssh root@135.181.4.110 systemctl stop 8bitnews-hub-vm 8bitnews-hub-network
```

Полностью удалить (VM, диск, сетевую изоляцию, systemd-юниты, сайт Caddy — **не трогая**
`/etc/caddy/Caddyfile`, UFW или что-либо соседа):

```sh
ssh root@135.181.4.110 bash <<'EOF'
systemctl disable --now 8bitnews-hub-vm 8bitnews-hub-backup.timer 8bitnews-hub-network
rm -f /etc/systemd/system/8bitnews-hub-vm.service \
      /etc/systemd/system/8bitnews-hub-network.service \
      /etc/systemd/system/8bitnews-hub-backup.service \
      /etc/systemd/system/8bitnews-hub-backup.timer
systemctl daemon-reload
rm -f /etc/caddy/sites/8bitnews.caddy
caddy validate --config /etc/caddy/Caddyfile && systemctl reload caddy
rm -rf /var/lib/8bitnews-vm /run/8bitnews-hub-vm /var/backups/8bitnews /opt/8bitnews /etc/8bitnews-backup.env
userdel 8bitnews-vm
EOF
```

Данные в R2 (медиа и бэкапы) этим не удаляются — удалите бакеты в Cloudflare отдельно, если нужно.

## 8. CI-джоба (описание для человека — `.github/workflows/` этой задачей не создаётся)

Токен платформы, которым написан этот код, не имеет scope `workflow`, поэтому файл workflow нужно
добавить руками. Минимальная джоба на PR, трогающих `deploy/ansible/**` или `internal/storage/**`:

```yaml
# .github/workflows/deploy-check.yml (добавить руками)
name: deploy-check
on:
  pull_request:
    paths:
      - deploy/ansible/**
      - internal/storage/**
jobs:
  deploy-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make deploy-check
      - run: go test ./internal/storage/...
```

`make deploy-check` сам поднимает всё, что ему нужно, через `docker run` (ansible-lint, caddy,
nft) — раннеру нужен только Docker, уже есть на `ubuntu-latest`.
