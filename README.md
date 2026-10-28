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

## Notas

- Os leitores só enviam comandos de consulta; nada é gravado nos dispositivos.
- O dongle MCHOSE responde apenas ao frame `AA` (o `65 01` do X9 não funciona neste PID).
- `debug.SetMemoryLimit(16 MiB)` mantém o heap do Go baixo.
- Este PC não tem Bluetooth e nenhum aparelho expõe a página de bateria HID padrão
  nem a propriedade de bateria do Windows — por isso o caminho é o protocolo de
  cada fabricante (HID++ para Logitech, frame `AA` para MCHOSE).
- Para sondar o HID sem conflito, pare o app antes (`Stop-Process batterytray`)
  e reinicie depois.
