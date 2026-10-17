# Kontrakt runnera testowego — PLAT-44

To przygotowanie granicy runnera dla platformctl i platformctl-templates. Skrypt
`scripts/sandbox_contract.py` tworzy wyłącznie nowy lokalny katalog i porównuje
prywatne pliki z obserwacją dostarczoną przez zaufany runner. Nie uruchamia
poleceń, Kind, Terraform, AWS, apply ani destroy. Nie odpytuje Docker/Kubernetes.
Pozytywny wynik zawiera `contract_verified:true`, ale zawsze
`live_verified:false` i `execution_authorized:false`.

PLAT-44 pozostaje częściowo wykonane. FP-29 dostarcza rzeczywiste uprawnienia
infrastruktury i warunki uruchomienia runnera. PLAT-30 implementuje E2E oraz
adapter zbierający obserwacje i wykonujący kroki; PLAT-20/26 nadal odpowiadają za
kontrolę celu i ownership wewnątrz lifecycle CLI. Ten kontrakt ich nie zastępuje.
Nie jest mechanizmem dystrybucji CLI ani macierzą wersji PLAT-11/36.

## Nowy run i środowisko

Użyj kanonicznej ścieżki absolutnej w prywatnym katalogu runnera. Rodzic musi
istnieć; docelowy katalog nie może istnieć. Na macOS `/tmp` jest symlinkiem,
więc użyj `/private/tmp` albo innego kanonicznego katalogu. Nie używaj primary
checkoutu, domowego katalogu operatora ani istniejącego workspace Symphony.

```sh
python3 scripts/sandbox_contract.py init \
  --root /private/tmp/plat44-example-run \
  --owner GCICHON-DEV/platformctl:PLAT-44
```

`init` zwraca nowy losowy 32-znakowy `run_id`, owner repo/zadanie, UID, pełną
ścieżkę oraz czas ważności dwóch godzin. Nazwy są deterministyczne względem
run-id: `platformctl-e2e-RUN_ID` i context `kind-platformctl-e2e-RUN_ID`.
Brak adoptowania istniejącego katalogu/klastra. Owner i run-id muszą następnie
pochodzić z niezależnego kontekstu joba, nie być bezwarunkowo kopiowane z
odczytywanego pliku podczas każdej kontroli.

```sh
python3 scripts/sandbox_contract.py environment \
  --root /private/tmp/plat44-example-run \
  --owner GCICHON-DEV/platformctl:PLAT-44 --run-id RUN_ID
```

Wynik to pełne środowisko procesu. Adapter przekazuje je jako `env=...`,
**nie scala z otoczeniem**, a `cwd` ustawia na `ROOT/work`. Ścieżki są stałe:

| Zasób | Ścieżka pod ROOT |
| --- | --- |
| HOME, TMPDIR | home, tmp |
| KUBECONFIG | kubeconfig.json |
| CLI state/lock | work/.platformctl |
| Terraform data/plugin directory | terraform |
| Lokalny Terraform state | pod work, zwykle work/generated/terraform |
| Toolchain/PATH | toolchain/bin oraz /usr/bin:/bin |
| AWS config/credentials | pusty disabled.aws, metadata wyłączone |
| Azure/GCloud/Docker/Helm config | osobne katalogi azure/gcloud/docker/helm |
| Dowody | evidence |

Brak dziedziczenia AWS_PROFILE/keys, GITHUB_TOKEN, KUBECONFIG, DOCKER_HOST,
TF_CLI_ARGS i nadpisania rejestru templates. Narzędzia muszą pochodzić z
zaakceptowanego toolchainu runnera. Kontrakt odrzuca symlinki, hardlinki,
pliki specjalne, obcy UID i zapis grupy/innych w drzewie sandboxu; toolchain
musi zawierać zwykłe pliki. ROOT ma 0700, pliki kontrolne 0600.

Izolacja zmiennych nie jest sandboxem systemowym. Ten sam UID może zmienić
pliki, template może wykonać dowolne polecenie, a dostęp do shared Docker socket
może dotknąć innych kontenerów. Dlatego do realnego E2E wymagany jest oddzielny,
jednorazowy runner/VM bez credentials dev/prod, bez dostępu do ich state oraz
bez cloud metadata/sekretów przez sieć. Nie używać uprzywilejowanego DinD w
klastrze utrzymaniowym. Obecny współdzielony runner 2 GiB i Docker operatora nie
zostają tym PR zatwierdzone do realnego lifecycle E2E. Wybór i capacity runnera,
konfigurację dostępu oraz okno zmian należy dopiero odebrać; bez zmian lease.

## Target i ownership

Zaufany adapter PLAT-30 przed utworzeniem klastra potwierdza brak docelowej
nazwy, a po utworzeniu zapisuje wyłącznie jego kubeconfig jako JSON w
ROOT/kubeconfig.json (0600). Dozwolony jest jeden context/cluster/user o nazwie
wynikającej z run-id, jawny HTTPS `127.0.0.1:PORT`, osadzone CA/cert/key.
Brak exec/auth-provider/tokenów i zewnętrznych ścieżek credentials. Prywatny
kubeconfig nie trafia do stdout, publicznego artefaktu ani repozytorium.

Adapter odczytuje aktualne inventory Dockera i oznaczenia własności utworzonych
w tym runie węzłów. Buduje prywatną obserwację JSON z dokładnie tymi polami:

- `version: 1`, `owner`, `run_id`, `cluster_name`, `context`;
- `server` identyczny z kubeconfig oraz `kubeconfig_sha256` liczony z jego bajtów;
- `nodes`: 1–10 rekordów `{id, cluster_name, owner, run_id}`. `id` to pełny
  64-znakowy container ID; pozostałe wartości pochodzą z rzeczywistego inventory
  i oznaczeń ownership, nie z kopiowania oczekiwanej deklaracji.

```sh
python3 scripts/sandbox_contract.py check \
  --root /private/tmp/plat44-example-run \
  --owner GCICHON-DEV/platformctl:PLAT-44 --run-id RUN_ID \
  --observation /private/path/current-observation.json
```

Brak/mismatch, obcy node, zmieniony kubeconfig, dodatkowy context, credentials
spoza fixture lub wygaśnięcie dają exit 2. Stdout zawiera sanitizowany błąd,
bez treści credentials. Skrypt nigdy nie usuwa katalogu ani danych po odmowie.

Obserwacja jest wejściem zaufanego adaptera, nie podpisanym dowodem. Sam plik,
prefix nazwy czy loopback endpoint nie dowodzą faktycznej własności zasobów.
Przed mutacją i cleanup adapter musi ponownie odczytać live inventory, porównać
pełne IDs/ownership i utrzymać wyłączność runnera. Sam wynik `check` nie jest
zezwoleniem na wykonanie późniejszego polecenia ani zabezpieczeniem przed TOCTOU.

## Awaria, anulowanie i odbiór

PLAT-30 ma zapisać zredagowany raport etapów, SHA pary CLI–templates i inventory
w evidence również po failure/cancel. Wtedy cleanup może objąć tylko zasoby
z potwierdzonymi IDs i ownership tego run-id; żaden globalny prune/list-and-delete.
Niepewny wynik, mismatch lub wygaśnięty kontrakt oznaczają zatrzymanie cleanup,
zachowanie state/kubeconfig i raport do operatora. Upływ TTL nie zwalnia lease
ani nie upoważnia do automatycznego destroy. Kubeconfig/state wymagają prywatnej
retencji poza publicznym artefaktem, aż do ustalenia rzeczywistego stanu.

Cloud E2E PLAT-31–35 wymaga osobnego zatwierdzonego sandboxu, konta/regionu,
własnych ról/state, TTL i budżetu oraz inventory kosztów/cleanup. Ten skrypt
obsługuje tylko `kind-fixture` i odrzuca tryb AWS. Nie wolno używać wspólnego
klastra FP/PA/MAT jako fixture destroy ani podstawiać jego state/roles.

Lokalnie sprawdzono testy odmowy i proces-stub z pełnym replacement environment:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_sandbox_contract.py' -v
```

Testy są odkrywane również przez istniejący CI `scripts/test_*.py`; workflow
nie zmieniono. Testy używają tymczasowych plików, syntetycznych certyfikatów
oraz zwykłego procesu Python; nie uruchamiają Kind/Docker/Terraform/AWS.
Nie wykonano live admission, awarii klastra, anulowania E2E ani cleanup zasobów.
To pozostaje kryterium odbioru PLAT-44/30 po udostępnieniu właściwego runnera.

Rollback przygotowania: wycofać skrypt/dokumentację. Istniejące runtime CLI,
manifesty templates i infrastruktura są bez zmian. Katalogów rozpoczętych
runów nie usuwać automatycznie podczas rollbacku.
