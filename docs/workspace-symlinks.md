# PLAT-15: granice workspace na Linux i macOS

Źródło: PLAT-15. Baza CLI: `29e35b1b0aa6273f330df6d474b17dc9b592cf13`.
Kontynuacja zatrzymanego kandydata `8853ee75dab9399486af6563a5e771cbaaf91d81`:
review wskazało regresję P1, ponieważ wszystkie operacje na macOS były odrzucane.

`internal/safepath` otwiera katalogi komponent po komponencie z NOFOLLOW.
Odczyt source wymaga zwykłego pliku; staging powstaje wyłącznie przez O_EXCL.
Rodzice zapisu/usuwania/rename są przypięci deskryptorami. Linux używa ścieżek
`/proc/self/fd`, macOS natywnych Openat/Renameat/Unlinkat. Nadanie uprawnień
pliku 0000 na macOS używa Fchmodat z AT_SYMLINK_NOFOLLOW. Końcowy symlink
nie prowadzi do zmiany uprawnień pliku poza workspace.

Testowy hook beforeRename może zasymulować błąd lub podmianę ścieżki, lecz nie
wykonuje właściwego rename; safepath zawsze kontroluje operację systemową.
Pozwala to sprawdzać rollback i race również na macOS, bez emulowania /proc.

Runner przypina katalog przed logowaniem i rozwiązywaniem programu. macOS
uruchamia Cmd na przypiętym OS thread z PthreadFchdir(cwd fd), następnie resetuje
katalog wątku przez PthreadFchdir(-1). Inne goroutines nie zmieniają cwd. Gdyby
reset nie powiódł się, goroutine kończy się z zablokowanym wątkiem, który Go
usuwa zamiast zwracać do puli. Dziedziczony workflow lock pozostaje deskryptorem3.
Linux nadal używa procfd jako cwd przed exec. Windows i inne systemy odmawiają
operacji; nie wprowadzono słabszego fallbacku na walidację leksykalną.

Polityka zaufania pozostaje w README: szablon uruchamia dowolne komendy z prawami
użytkownika. Nie jest sandboxem. Ochrona nie obejmuje hardlinków, mountów,
podmiany zawartości przez inny proces ani przenoszenia przypiętych katalogów
poza workspace przez proces z tymi samymi prawami.

## Weryfikacja 2026-10-24

- macOS arm64, Go1.27.3: go vet ./..., pełne go test ./..., pełne go test -race ./...,
  build oraz kontrakt3szablonów (9/1/5plików) PASS. Bez pomijania testów httptest.
- Linux arm64, Go1.26.9 w istniejącym obrazie Symphony: identyczne vet/unit/race/
  build/check-templates PASS. Kontener bez sieci, bez capabilities, z read-only
  kodem i izolowanym wykonywalnym tmpfs. Pierwszy setup miał nie-wykonywalne /tmp
  i zakończył się permission denied; po poprawie samego fixture powyższe kontrole
  przeszły. Bez zmian runtimeDB lub workera Symphony.
- Wspólne testy safepath obejmują parent/leaf symlink, podmianę rodzica w rename,
  podmianę leaf, współbieżną podmianę source i staging0000. Testy render/reconcile
  oraz runnera potwierdzają nietkniętą zewnętrzną fixture. macOS test potwierdza
  cwd dziecka oraz brak zmiany katalogu głównego procesu.
- Własna binarka macOS: lokalny syntetyczny plan → apply → destroy PASS (wyłącznie
  pliki i shell markers). Próby parent/leaf/source symlink, stale reconcile oraz
  apply po podmianie generated odmówione. Hash zewnętrznych plików bez zmian,
  brak markerów poza workspace. To nie Kind/AWS E2E.
- Utrzymano go.mod1.22. x/sysv0.30.0 obsługuje Go1.18; dodano go.sum i użyto
  systemowych wrapperów Darwin bez podnoszenia minimalnego Go. To pin zgodności,
  nie deklaracja najnowszej wersji biblioteki.

Prywatne dowody syntetycznego CLI: `.delivery-evidence/plat-15-20261024/` poza repo.
Źródłowe checkpointy i runtimeSQLite pozostają niezmienione. Brak cloud apply,
zmiany zasobów, wdrożenia ani instalacji globalnej. Wycofanie binarki nie cofa
infrastruktury lub state. PR/review/CI i wydanie po synchronizacji dokładnegoSHA
pozostają bramkami finalizacji.
