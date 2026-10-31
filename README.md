# Bateria dos Dispositivos (bandeja, Go)

Interface única na bandeja do Windows com a bateria dos periféricos, **sem instalar
M HUB, G HUB ou qualquer software oficial** — leitura HID direta e somente-consultas.
Os aparelhos são **descobertos automaticamente**, sem cadastro no código:

| Fonte | Como funciona |
|---|---|
| **Logitech (genérico)** | Varre todos os receptores `046D:*` com coleção HID++ (`0xFF00/0x0002`) e sonda os índices 1–6 de aparelhos pareados. Nome vem da feature `0x0005` (ex.: *"G Pro Wireless Gaming Mouse"*); bateria pela `0x1004` (percentual) com queda para `0x1001` (voltagem → LUT mV→%, LGSTrayBattery/Solaar) |
| **MCHOSE V9 Pro 2** (`3837:6011`) | Frame `AA` do M HUB: `55 AA 01 00 01 00 01 0B 00` na coleção vendor `0xFF07/0x222` → nível no payload. Nome vem da string HID do próprio dongle |

Qualquer mouse/teclado/fone Logitech wireless pareado ao receptor aparece sozinho
no menu — inclusive vários ao mesmo tempo.

## Consumo

| | Memória residente | CPU |
|---|---|---|
| **Esta versão (Go)** | **~14–20 MB**, 1 processo | 0,02% de 1 core (~20 ms a cada 60 s) |
| Versão anterior (Node + systray2) | 80 MB, 2 processos | 0,02% |

O binário tem 4,2 MB, embute o ícone (`go:embed`) e não precisa de runtime instalado.

> Observação: o contador *Bytes Privados* do Windows mostra ~46 MB, mas ~27 MB
> estão pagados fora (não ocupam RAM). Esses 46 MB são do próprio `systray` no
> registro da bandeja — verificado com um programa mínimo que só chama
> `systray.Run` (sem menu): dá os mesmos 46 MB, com o Go em 6 MB. É custo
> pré-existente da biblioteca, não do código daqui, e não cresce com o tempo.

## Uso

```powershell
cd C:\Users\mathe\battery-tray-go
.\batterytray.exe
```

- O menu (clique direito no ítem) lista **cada dispositivo descoberto com seu
  nível** no topo, depois a hora da última leitura, **Atualizar agora** e **Sair**.
- **Menu dinâmico**: os itens são reservados em slots no topo antes dos controles
  (o systray ordena o menu pelo ID do item), então um aparelho descoberto depois —
  ex.: o mouse acordando após o app iniciar — entra no topo, e não depois de "Sair".
- Um aparelho que entra em sono **mantém o item** e passa a mostrar
  "dormindo / sem nível"; o item só some se o dongle/receptor sair.
- **Notificar bateria baixa (10% e 5%)** — checkbox no menu, ligado por padrão.
  Cada limiar dispara **uma vez por dispositivo** (estado indexado pelo ID do
  aparelho); re-arma sozinho quando o nível sobe (ex.: foi para carregar).
  Preferência persistida em `batterytray.cfg` ao lado do exe.
- **Testar notificação** — mostra um aviso de exemplo sem precisar drenar a bateria.
  (Também dá para disparar via variável `BATTERYTRAY_TEST=1` na inicialização.)
- Leitura automática a cada 60 s; os dispositivos são lidos em sequência.
- Inicialização automática: atalho `Bateria Dispositivos.lnk` na pasta Startup
  (aponta para este exe).
- Log de diagnóstico em `batterytray.log` (ao lado do exe).

## Compilar

Para gerar tudo (app + instalador + zip da release), use o script:

```powershell
cd C:\Users\mathe\battery-tray-go
powershell -ExecutionPolicy Bypass -File build.ps1 -Version 1.0.2
```

O script faz `go vet` + `gofmt`, gera os recursos Win32 (ícone, VersionInfo e
manifestação com `go-winres`), compila o `batterytray.exe`, monta o
`dist\Setup.exe` (com o app embutido) e o `.zip` — e imprime os hashes SHA256.

Build manual só do app:

```powershell
cd C:\Users\mathe\battery-tray-go
$env:CGO_ENABLED='1'
go build -ldflags "-s -w -H windowsgui" -o batterytray.exe .
```

> **`-H windowsgui` é obrigatório**: sem ela o Windows abre um console preto ao
> lado da bandeja (e fechar esse console mata o app). Com ela o binário é um
> app de GUI puro, sem janela de terminal.

Requer Go + GCC (para o `hidapi` em cgo). Dependências: `getlantern/systray`,
`sstallion/go-hid`.

## Arquivos

- `main.go` — bandeja, menu dinâmico (slots), ciclo de atualização e log
- `devices.go` — tipo `Device` e reunião das fontes (Logitech + MCHOSE)
- `logitech.go` — descoberta e leitor HID++ genérico (nome, voltagem/percentual)
- `mchose.go` — leitor do headset (frame AA do M HUB, com checksum)
- `alert.go` — limiares 10% / 5% por ID de dispositivo e persistência
- `notify.go` — toast Win32 próprio (janela sem foco, thread fixa, sem processos)
- `icon.ico` — ícone embutido no binário
- `installer/setup.cs` — instalador WinForms (compilado com o `csc` do Windows);
  `installer/setup.manifest` — manifestação `asInvoker` do instalador
- `winres/` — configuração do `go-winres` (ícone, VersionInfo e manifestação
  do app; o `.syso` gerado é ignorado pelo git)
- `build.ps1` — build completo: vet/gofmt → recursos Win32 → app → Setup → zip
- `probe/` — sonda de hardware (não entra no exe); ver abaixo

## Sonda de hardware (`probe/`)

Ferramenta de diagnóstico para descobrir o que um periférico realmente expõe.
Não faz parte do aplicativo (`package main` próprio) e só envia consultas:

```powershell
$env:CGO_ENABLED='1'
go run ./probe      # fases principais
go run ./probe 8    # uma fase específica (1,2,5,8,9,10,11)
```

| Fase | O que faz |
|---|---|
| 1 | Inventário das interfaces HID + procura de página de bateria (`0x04`/`0x06`) |
| 2 | Receptores Logitech: ping dos índices 1–6 + features de bateria |
| 5 | Strings HID por interface (fonte de nomes de graça) |
| 8 | Nome do aparelho via feature `0x0005` (codificação correta) |
| 9 | Comparação de ping de 7 vs 8 bytes |
| 10 | Leitura bruta sem filtro (diagnóstico de "sem resposta") |
| 11 | Nome completo em blocos de 16 bytes + tipo/fw do ping |

> **Formato HID++ validado**: report longo `11 | devIdx | featIdx | (fn<<4)|swID |
> p0 p1 p2 | swID`, com `swID=0x0A`. A função vai no **nibble alto** do byte 3 —
> uma versão antiga do probe a colocava no nibble baixo e o aparelho sempre
> executava a função 0 (toda resposta saía igual).

## Antivírus, SmartScreen e "app não assinado"

Programas **sem assinatura digital de código** são frequentemente sinalizados
por antivírus — principalmente binários novos, compilados em Go (UPX-like
stripping com `-s -w`) e instaladores que extraem um exe e criam um item de
inicialização automática. **Isso não é vírus**: o programa só consulta os
periféricos por HID e não baixa nem grava nada. Assinatura digital (certificado
OV/EV ou SignPath para open source) é o que elimina de vez esses avisos.

Enquanto isso, o que fazer:

### Permitir o app no Microsoft Defender

1. **SmartScreen** (janela azul "Windows protegeu o PC"): clique em
   *Mais informações → Executar assim mesmo*. O aviso some depois de o app
   ser usado/instalado normalmente.
2. **Defender bloqueou o arquivo**: *Segurança do Windows → Proteção contra
   vírus e ameaças → Proteções contra vírus e ameaças → Adicionar ou remover
   exclusões* e inclua a pasta de instalação
   (`%LOCALAPPDATA%\BatteryTray`) — ou o arquivo baixado, antes de rodar.
3. Depois de permitir, pode também enviar o arquivo ao Microsoft para análise
   (ver abaixo).

### Pedir revisão ao Microsoft (gratuito)

Submeta os arquivos em
[https://www.microsoft.com/wdsi/filesubmission](https://www.microsoft.com/wdsi/filesubmission)
(preferir **"I'm a developer..."** para ter acompanhamento por e-mail):

- `Setup.exe` (instalador, ~4,3 MB)
- `batterytray.exe` (app, ~4,3 MB)

Na descrição, em inglês, explique brevemente: *"Open-source battery monitor
for Logitech/MCHOSE peripherals. Reads battery level over HID (read-only);
no network, no persistence besides an optional Startup shortcut. No code
signing certificate available (free project)."* Inclua o link do repositório
GitHub — o código aberto ajuda muito na análise.

O mesmo processo vale para outros fabricantes (Kaspersky, Avast/AVG,
Bitdefender etc.): todos têm formulário público de "falso positivo" em seus
sites, e o código aberto no GitHub é o principal argumento.

### Hashes dos arquivos (v1.0.2)

Confira se o arquivo baixado bate com estes hashes ( PowerShell:

```powershell
Get-FileHash .\Setup.exe -Algorithm SHA256
Get-FileHash .\batterytray.exe -Algorithm SHA256
```

| Arquivo | SHA256 |
|---|---|
| `batterytray.exe` | `A626228ADE49AFBF4237ADD82309E46853586CC7BD140560D2525EAC43B3B510` |
| `Setup.exe` | `A9E2A56A664AE0B9A5D0A91F4DCD7C5FAC52F274C0BE6E0B9394A88A8300FD9A` |

> Cada build gera hashes diferentes — os acima valem para a release v1.0.2.

### O que já foi feito para reduzir as detecções (v1.0.1)

- **Manifestação explícita** nos dois exes (`asInvoker`: nunca pede
  admin; comctl32 v6 + DPI awareness).
- **VersionInfo completo** (empresa, descrição, copyright, versão 1.0.1.0)
  em `batterytray.exe` (via `go-winres`) e no `Setup.exe` (atributos de
  assembly) — arquivos "anônimos" sem metadados chamam mais atenção.
- **Build reproduzível**: o `build.ps1` gera tudo a partir do código, então
  qualquer pessoa pode conferir que o binário da release vem daqui.

## Notas

- Os leitores só enviam comandos de consulta; nada é gravado nos dispositivos.
- O dongle MCHOSE responde apenas ao frame `AA` (o `65 01` do X9 não funciona neste PID).
- `debug.SetMemoryLimit(16 MiB)` mantém o heap do Go baixo.
- Este PC não tem Bluetooth e nenhum aparelho expõe a página de bateria HID padrão
  nem a propriedade de bateria do Windows — por isso o caminho é o protocolo de
  cada fabricante (HID++ para Logitech, frame `AA` para MCHOSE).
- Para sondar o HID sem conflito, pare o app antes (`Stop-Process batterytray`)
  e reinicie depois.
