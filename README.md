# Klient

Zdrojový kód: <https://github.com/Imbecile6197/klient> · licence GPL-3.0

Kompletní uživatelská dokumentace: [`docs/index.html`](docs/index.html). V aplikaci ji otevře hlavní nabídka → Nápověda nebo klávesa F1.

E-mailový klient pro GNOME (GTK4 + libadwaita) napsaný v Go. Umí:

- **Proton Mail nativně**: přímo přes Proton API (SRP přihlášení, 2FA, režim dvou hesel). Proton Bridge není potřeba.
- **Nativní PGP**: dešifrování a ověření podpisu u každé zprávy. Při odesílání se pro každého příjemce zvolí nejsilnější dostupná ochrana:
  - Proton adresy dostanou end-to-end šifrování Protonu,
  - externí adresy s klíčem (WKD nebo lokálně importovaným) dostanou PGP,
  - ostatní adresy dostanou nešifrovanou zprávu, volitelně s PGP podpisem, a před odesláním se zobrazí varování.
- **AI asistenta**: shrnutí zprávy, návrh odpovědi, úprava nebo překlad konceptu. Podporovaní poskytovatelé jsou **Claude (Anthropic)**, **ChatGPT (OpenAI)**, **Gemini (Google)** a **Mistral** (servery v EU). Asistent a spamfiltr můžou používat různé poskytovatele a modely.
- **Běžné funkce klienta**:
  - odpovědět, odpovědět všem, přeposlat (i s přílohami),
  - **odesílání příloh** (šifrují se stejně jako text zprávy, limit 25 MB),
  - koncepty (automaticky se uloží při zavření okna),
  - podpis,
  - našeptávání adres z kontaktů Protonu,
  - hvězdička, označení jako nepřečtené, archiv,
  - vlastní složky a štítky Protonu,
  - vyhledávání,
  - **vlákna (konverzace)**: seskupení jako na webu Protonu; starší zprávy se dešifrují až po rozbalení, vypnout jde v Předvolbách → Zprávy,
  - klávesové zkratky (Ctrl+? je vypíše),
  - **HTML zprávy** přes WebKitGTK: bez JavaScriptu, vzdálené obrázky a sledovací pixely blokované (povolit jde pro zprávu nebo odesílatele),
  - **formátovaný editor**: tučné, kurzíva, podtržení, seznamy, odkazy. Příjemci s PGP dostanou textovou verzi,
  - hromadný výběr (tlačítko, Ctrl/Shift+klik, Ctrl+A),
  - **šifrovaná offline cache** posledních zpráv (AES-256-GCM, klíč v klíčence, těla navíc PGP),
  - zpoždění odeslání s tlačítkem Zpět, pravidla pro příchozí poštu, odhlášení odběru (RFC 8058),
  - tisk, export .eml, výchozí aplikace pro odkazy `mailto:`,
  - **více účtů Proton** v jednom okně (přepínač v postranním panelu); nová pošta, spamfiltr a oznámení fungují pro všechny účty,
  - **automatické aktualizace z GitHubu**: nová verze se na pozadí stáhne a ověří kontrolním součtem, nainstaluje se jedním kliknutím a heslem správce,
  - **běh na pozadí s ikonou v liště** (GNOME potřebuje rozšíření AppIndicator), volitelné spuštění po přihlášení,
  - **naplánované odeslání** (na serveru Protonu, odejde i při vypnutém počítači) s odpočtem; odpočet i u zpoždění odeslání,
  - **odložení konverzace** – vrátí se do doručené pošty v zadaný čas,
  - přetahování souborů do zprávy a zpráv do složek a štítků,
  - **pozvánky do kalendáře** (.ics): přijmout / možná / odmítnout, přidat do kalendáře,
  - **automatická odpověď v nepřítomnosti** (na serveru, vyžaduje placený tarif),
  - přiložení vlastního veřejného klíče a import klíče z přílohy.
- **Další poštovní služby**: Seznam.cz (seznam.cz, email.cz, post.cz, spoluzaci.cz), Gmail (heslo pro aplikace; štítky fungují jako na webu – přidání zprávu jen označí, archivace ji přesune do Všech zpráv) a libovolná schránka s IMAP/SMTP. Servery se dohledají automaticky. Nová pošta přichází okamžitě přes IMAP IDLE, ale stejně jako u Protonu se ukáže až po kontrole spamfiltrem. Hledání prohledává i text zpráv. Zprávy se seskupují do vláken (podle hlaviček References/In-Reply-To, napříč složkami). Odložení a naplánované odeslání obstarává Klient sám přes složky „Odložené“ a „Naplánované“ na serveru – fungují, když Klient běží (třeba na pozadí), a zmeškané se provedou hned po spuštění. **PGP i pro tyto služby**: v Předvolbách → PGP si vytvoříte nebo naimportujete vlastní klíč. Zprávy lidem se známým klíčem (Web Key Directory jejich domény, hlavička Autocrypt, importované klíče, volitelně keys.openpgp.org) se šifrují ve formátu PGP/MIME, ostatním se podepisují; smíšeným příjemcům odejdou dvě verze. Koncepty, naplánované zprávy a kopie v Odeslaných se na serveru ukládají zašifrované jen pro vás. Příchozí PGP/MIME zprávy se dešifrují a ověřují. Automatická odpověď u těchto služeb chybí. Pokud server neuvádí limit schránky (Seznam), ukazuje se aspoň obsazené místo.
- **Lokální AI (Ollama)**: model běží přímo v počítači a obsah zpráv nikam neodchází. Klient si stáhne oficiální Ollamu do `~/.local/share/klient/ollama` (ověří kontrolní součet, bez sudo, jen pro procesor) a spouští ji jen pro sebe na 127.0.0.1. **Automaticky ji aktualizuje** i s používanými modely (jednou denně, ne na měřeném připojení). Na výběr jsou Qwen 3.5, Gemma 4 a Ministral 3. Modely jde **porovnat na vlastní poště** a režim **„Jen lokálně“** zakáže jakoukoli cloudovou AI.
- **AI navíc**: „Zeptat se pošty“ (Ctrl+J) odpoví z nalezených zpráv s odkazy na zdroje, automatické třídění nové pošty do štítků a ranní přehled nepřečtené pošty.
- **Lokální spamfiltr řízený AI**: každou novou zprávu v doručené poště posoudí AI. Jako podklady dostane:
  - SPF/DKIM/DMARC,
  - značky Protonu,
  - zásahy IP, domén a odkazů v blocklistech,
  - vaše seznamy povolených a blokovaných odesílatelů.
- **Blocklisty** se automaticky stahují **každých 12 hodin**: Spamhaus DROP (IPv4/IPv6), URLhaus a OpenPhish.

## Sestavení

```bash
sudo dnf install golang gcc gtk4-devel libadwaita-devel gobject-introspection-devel webkitgtk6.0-devel
make build        # první build trvá dlouho (CGo bindingy GTK)
make run
make install      # do ~/.local, včetně .desktop souboru a ikony
make rpm          # balíček pro Fedoru do build/rpm/RPMS/x86_64/
```

Instalace nejnovější verze přímo z GitHubu (Fedora):

```bash
sudo dnf install https://github.com/Imbecile6197/klient/releases/latest/download/klient.x86_64.rpm
```

Další verze si nainstalovaný Klient stáhne z [vydání na GitHubu](https://github.com/Imbecile6197/klient/releases) sám.

Vydání nové verze (vyžaduje `gh` a commitnutý strom): `git tag -a vX.Y.Z -m "Klient X.Y.Z" && make release VERSION=X.Y.Z`.

## První spuštění

1. Přihlaste se účtem Proton. Heslo se neukládá. V klíčence GNOME (Secret Service) zůstane jen:
   - token relace,
   - odvozený klíč pro odemčení schránky.
2. **Předvolby → AI**: pro asistenta a pro spamfiltr zvolte poskytovatele a vložte jeho API klíč. Tlačítko u každého klíče otevře stránku, kde klíč získáte. Klíče se ukládají do klíčenky.
3. Volitelně v nabídce spusťte **Prověřit doručenou poštu pomocí AI**. Filtr se tím spustí nad posledními 50 zprávami.

## Struktura

| Balíček | Účel |
|---|---|
| `internal/protonmail` | Přihlášení, odemčení klíčů, seznam a dešifrování zpráv, přílohy, šifrované odesílání, stream událostí |
| `internal/pgp` | Lokální úložiště veřejných klíčů kontaktů, ověřování podpisů (včetně inline cleartext) |
| `internal/ai` | Poskytovatelé Claude, OpenAI, Gemini a Mistral (oficiální SDK; Mistral přes OpenAI SDK) za společným rozhraním. Klasifikace spamu přes JSON schéma, shrnutí, návrhy odpovědí |
| `internal/spam` | Blocklisty a jejich aktualizace, spamfiltr (důkazy → AI → rozhodnutí), seznamy povolených a blokovaných |
| `internal/mailparse` | MIME a HTML → text. HTML se nevykresluje, takže se nenačítá vzdálený obsah ani sledovací pixely |
| `internal/ui` | GNOME UI: trojpanelové rozvržení s breakpointy, psaní zpráv, předvolby |

Soubory:

- nastavení: `~/.config/klient/config.json`
- data (blocklisty, rozhodnutí filtru, klíče kontaktů): `~/.local/share/klient/`

## Soukromí a omezení

- **Obsah e-mailů odchází ke zvolenému AI poskytovateli** (spamfiltr, shrnutí a návrhy). U OpenAI se posílá s `store: false`. Pro spamfiltr se posílá jen prvních 6000 znaků těla (`spam_body_chars`). Ukládání a zpracování dat řídí API zásady daného poskytovatele. Pokud to nechcete, vypněte v Předvolbách AI filtr. Klient pak pozná spam jen podle blocklistů a vašich seznamů.
- Výchozí model je `claude-opus-5` pro asistenta i filtr. Klasifikace každé příchozí zprávy tak stojí peníze. V Předvolbách můžete pro filtr nastavit levnější model (např. `claude-haiku-4-5`).
- Proton API není pro klienty třetích stran oficiálně dokumentované a může se měnit. Aplikace se identifikuje hlavičkou `x-pm-appversion` podle `proton_app_version` v konfiguraci (výchozí `Other`). Proton může přihlášení odmítnout nebo vyžádat CAPTCHA. V tom případě se jednou přihlaste přes web ze stejné sítě.
- U Protonu vyhledávání prochází předmět, odesílatele a příjemce, ne text zprávy. Těla zpráv jsou na serveru end-to-end šifrovaná, takže fulltext by potřeboval lokální index.
- Přílohy se do offline cache neukládají. Pravidla jsou společná pro všechny účty (cílové složky a štítky patří účtu, ve kterém vznikly).
- `third_party/gotk4-webkitgtk` je výběr Go bindingů WebKitGTK bez třídy, kterou WebKitGTK 2.54 odstranil (viz `KLIENT_PATCHES.md`). K sestavení je potřeba `webkitgtk6.0-devel`.
- `third_party/go-proton-api` je kopie knihovny Protonu s několika úpravami: pole pro konverzace, naplánované odeslání, odložení a automatická odpověď (viz `KLIENT_PATCHES.md`).
