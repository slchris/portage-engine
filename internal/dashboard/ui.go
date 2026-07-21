package dashboard

// ui.go holds the dashboard's entire frontend: a shared Apple-style token
// foundation (values verbatim from Apple's shipped App Store stylesheet), a
// common app shell, i18n (English default, Chinese via toggle/auto-detect),
// and every page template. Pages are assembled at init time by appPage() so
// chrome stays consistent. All dynamic rendering uses createElement/
// textContent — no innerHTML interpolation of API data.

// appleCSS is served at /static/apple.css. Ink is an alpha ladder, surfaces
// are two levels (recessed floor / raised card), separation is hairlines, the
// only motion is a 210ms ease-out nudge, and dark mode is pure token
// remapping via prefers-color-scheme.
const appleCSS = `:root {
  color-scheme: light dark;
  --font-family: -apple-system, BlinkMacSystemFont, "Apple Color Emoji", "SF Pro", "PingFang SC", "SF Pro Icons", "Helvetica Neue", Helvetica, Arial, sans-serif;
  --font-mono: ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace;

  /* Type ramp: Apple's rung structure, scaled up one step for dashboard
     readability (the original 13px body reads small for dense admin UI). */
  --header-emphasized:      700 38px/1.18 var(--font-family);
  --title-1-emphasized:     700 26px/1.2 var(--font-family);
  --title-2-emphasized:     700 20px/1.3 var(--font-family);
  --title-3-emphasized:     600 17px/1.35 var(--font-family);
  --title-3-tall:           400 17px/1.5 var(--font-family);
  --body:                   400 14px/1.35 var(--font-family);
  --body-emphasized:        600 14px/1.35 var(--font-family);
  --body-tall:              400 14px/1.5 var(--font-family);
  --body-bold-tall:         700 14px/1.4 var(--font-family);
  --callout:                400 13px/1.35 var(--font-family);
  --callout-emphasized:     600 13px/1.35 var(--font-family);
  --subhead-emphasized:     600 12px/1.3 var(--font-family);
  --footnote:               400 11px/1.35 var(--font-family);

  --systemPrimary:    rgba(0, 0, 0, .85);
  --systemSecondary:  rgba(0, 0, 0, .5);
  --systemTertiary:   rgba(0, 0, 0, .25);
  --systemQuaternary: rgba(0, 0, 0, .1);
  --systemQuinary:    rgba(0, 0, 0, .05);

  --systemRed:    #ff3b30;
  --systemOrange: #ff9500;
  --systemGreen:  #28cd41;
  --systemBlue:   #007aff;
  --systemGray:   #8e8e93;
  --systemGray6:  #f2f2f7;

  --keyColor: #007aff;
  --keyColor-rgb: 0, 122, 255;

  --pageFloor: #f5f5f7;
  --pageRaised: #fff;
  --navSidebarBG: rgba(60, 60, 67, .03);

  --labelDivider: rgba(0, 0, 0, .15);
  --keyline: .5px solid var(--labelDivider);
  --shadow-small:  0 3px  9px rgba(0, 0, 0, .08);
  --shadow-medium: 0 3px 20px rgba(0, 0, 0, .08);

  --radius-small: 9px;
  --radius-medium: 12px;
  --radius-large: 17px;
  --buttonRadius: 6px;

  --bodyGutter: 25px;
  --hover-transition: 210ms ease-out;
  --alpha-multiplier: 1;
}
@media (min-width: 1000px) { :root { --bodyGutter: 40px; } }

@media (prefers-color-scheme: dark) {
  :root {
    --systemPrimary:    hsla(0, 0%, 100%, .85);
    --systemSecondary:  hsla(0, 0%, 100%, .55);
    --systemTertiary:   hsla(0, 0%, 100%, .25);
    --systemQuaternary: hsla(0, 0%, 100%, .1);
    --systemQuinary:    hsla(0, 0%, 100%, .05);
    --systemRed:    #ff453a;
    --systemOrange: #ff9f0a;
    --systemGreen:  #32d74b;
    --systemBlue:   #0a84ff;
    --systemGray:   #98989d;
    --systemGray6:  #1c1c1e;
    --pageFloor: #151515;
    --pageRaised: #1f1f1f;
    --navSidebarBG: rgba(235, 235, 245, .03);
    --labelDivider: hsla(0, 0%, 100%, .1);
    --alpha-multiplier: 1.33;
  }
}
@supports not (font: -apple-system-body) {
  :root { --systemPrimary: rgba(0, 0, 0, .88); --systemSecondary: rgba(0, 0, 0, .56); }
  @media (prefers-color-scheme: dark) {
    :root { --systemPrimary: hsla(0, 0%, 100%, .92); --systemSecondary: hsla(0, 0%, 100%, .64); }
  }
}

* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  background: var(--pageFloor);
  color: var(--systemPrimary);
  font: var(--body);
  letter-spacing: 0;
  font-synthesis: none;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
a { color: var(--keyColor); text-decoration: none; }
:focus-visible { outline: 4px solid rgba(var(--keyColor-rgb), .6); outline-offset: 1px; }
@supports selector(:focus-visible) {
  a:focus, button:focus { box-shadow: none; outline: none; }
  a:focus-visible, button:focus-visible { box-shadow: 0 0 0 4px rgba(var(--keyColor-rgb), .6); outline: none; }
}

/* ---- pill buttons ---- */
.btn {
  border: 0; cursor: pointer; display: inline-block;
  border-radius: 1000px; padding: 7px 16px;
  font: var(--body-bold-tall); word-break: keep-all;
  transition: background-color .14s ease-out;
  background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .06));
  color: var(--keyColor);
}
.btn:hover  { background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .1)); transition: background-color .21s ease-out; }
.btn:active { background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .07)); }
.btn.blue        { background: var(--keyColor); color: hsla(0, 0%, 100%, .95); }
.btn.blue:hover  { background: color-mix(in srgb, var(--keyColor), #000 3%); }
.btn.blue:active { background: color-mix(in srgb, var(--keyColor), #000 6%); }
.btn[disabled] { opacity: .4; cursor: default; }
.lang-btn { border: 0; background: none; cursor: pointer; font: var(--callout); color: var(--systemSecondary); padding: 4px 8px; border-radius: 1000px; transition: background-color 175ms ease-in; }
.lang-btn:hover { background: var(--systemQuinary); }

/* ---- landing ---- */
.landing-nav {
  display: flex; align-items: center; justify-content: space-between;
  height: 48px; padding: 0 var(--bodyGutter);
  border-bottom: var(--keyline);
}
.landing-nav .brand { font: var(--body-emphasized); color: var(--systemPrimary); }
.landing-nav .side { display: flex; align-items: center; gap: 8px; }
.landing-hero { max-width: 720px; margin: 0 auto; padding: 96px var(--bodyGutter) 64px; text-align: center; }
.landing-hero .eyebrow { font: var(--subhead-emphasized); text-transform: uppercase; letter-spacing: .06em; color: var(--systemSecondary); margin-bottom: 12px; }
.landing-hero h1 { font: var(--header-emphasized); text-wrap: balance; margin-bottom: 14px; }
.landing-hero .sub { font: var(--title-3-tall); color: var(--systemSecondary); text-wrap: pretty; margin-bottom: 28px; }
.landing-hero .cta { display: flex; gap: 10px; justify-content: center; }
.landing-grid {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 20px;
  max-width: 1000px; margin: 0 auto; padding: 0 var(--bodyGutter) 64px;
}
@media (max-width: 800px) { .landing-grid { grid-template-columns: 1fr; } }
.landing-card {
  background: var(--pageRaised); border-radius: var(--radius-large);
  box-shadow: var(--shadow-small); padding: 20px; min-height: 150px;
}
@media (prefers-color-scheme: dark) { .landing-card { background: var(--systemQuaternary); } }
.landing-card h4 { font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemSecondary); margin-bottom: 3px; }
.landing-card h3 { font: var(--title-2-emphasized); margin-bottom: 6px; }
.landing-card p  { font: var(--body-tall); color: var(--systemSecondary); text-wrap: pretty; }
.landing-flow { max-width: 1000px; margin: 0 auto; padding: 0 var(--bodyGutter) 80px; }
.landing-flow h2 { font: var(--title-2-emphasized); margin-bottom: 13px; }
.landing-flow .steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 20px; }
@media (max-width: 800px) { .landing-flow .steps { grid-template-columns: 1fr; } }
.landing-flow .step h4 { font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemSecondary); margin-bottom: 4px; }
.landing-flow .step p { font: var(--body-tall); color: var(--systemSecondary); }
.landing-flow .step .mono { font: 400 12.5px/1.6 var(--font-mono); color: var(--systemPrimary); background: var(--systemQuinary); border-radius: var(--buttonRadius); padding: 8px 10px; margin-top: 8px; display: block; overflow-x: auto; white-space: nowrap; }
.landing-footer { border-top: var(--keyline); padding: 20px var(--bodyGutter); font: var(--footnote); color: var(--systemTertiary); text-align: center; }

/* ---- auth card (login) ---- */
.auth-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: var(--bodyGutter); }
.auth-card {
  width: 340px; background: var(--pageRaised); border-radius: var(--radius-large);
  box-shadow: var(--shadow-medium); padding: 28px;
}
@media (prefers-color-scheme: dark) { .auth-card { background: var(--systemQuaternary); } }
.auth-card .brand { font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemSecondary); margin-bottom: 6px; }
.auth-card h1 { font: var(--title-1-emphasized); margin-bottom: 18px; }
.auth-card .btn { width: 100%; margin-top: 6px; }
.auth-err { font: var(--callout); color: var(--systemRed); min-height: 15px; margin: 8px 0 2px; }
.auth-note { font: var(--callout); color: var(--systemTertiary); margin-top: 14px; text-align: center; display: flex; justify-content: center; gap: 10px; align-items: center; }

/* ---- form fields ---- */
.field { margin-bottom: 12px; }
.field label { display: block; font: var(--callout-emphasized); color: var(--systemSecondary); margin-bottom: 4px; }
.field input[type=text], .field input[type=password], .field input[type=number], .field select {
  width: 100%; height: 32px; padding: 6px 8px;
  font: var(--body); font-family: var(--font-family); color: var(--systemPrimary);
  background: var(--pageRaised); border: 1px solid var(--labelDivider); border-radius: var(--buttonRadius);
}
@media (prefers-color-scheme: dark) { .field input, .field select { background: var(--systemQuinary); } }
.field input:focus, .field select:focus { box-shadow: 0 0 0 4px rgba(var(--keyColor-rgb), .6); outline: none; }
.field input::placeholder, .field textarea::placeholder { color: var(--systemTertiary); }
.field textarea {
  width: 100%; min-height: 120px; padding: 8px;
  font: 400 12.5px/1.6 var(--font-mono); color: var(--systemPrimary);
  background: var(--pageRaised); border: 1px solid var(--labelDivider); border-radius: var(--buttonRadius);
  resize: vertical;
}
@media (prefers-color-scheme: dark) { .field textarea { background: var(--systemQuinary); } }
.field textarea:focus { box-shadow: 0 0 0 4px rgba(var(--keyColor-rgb), .6); outline: none; }
.field .hint { font: var(--footnote); color: var(--systemTertiary); margin-top: 3px; }
.field.check { display: flex; align-items: center; gap: 8px; }
.field.check label { margin: 0; font: var(--body); color: var(--systemPrimary); }
@media (max-width: 483px) { .field input, .field select { font-size: 16px; height: 38px; border-radius: var(--radius-small); } }

/* ---- app shell ---- */
.shell { display: flex; min-height: 100vh; }
.sidebar {
  width: 260px; flex-shrink: 0;
  background: var(--navSidebarBG);
  border-inline-end: 1px solid var(--labelDivider);
  padding: 20px 12px;
  display: flex; flex-direction: column; gap: 2px;
  position: sticky; top: 0; height: 100vh;
}
.sidebar .brand { font: var(--body-emphasized); padding: 4px 8px 16px; color: var(--systemPrimary); }
.sidebar .brand span { display: block; font: var(--footnote); color: var(--systemTertiary); margin-top: 2px; }
.nav-item {
  display: block; padding: 8px; border-radius: var(--radius-medium);
  font: var(--body); color: var(--systemPrimary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  transition: background-color 175ms ease-in;
}
.nav-item:hover { background: var(--systemQuinary); }
.nav-item.active { background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .08)); color: var(--keyColor); font: var(--body-emphasized); }
.sidebar .spacer { flex: 1; }
.sidebar .foot { padding: 8px; font: var(--footnote); color: var(--systemTertiary); }
.sidebar .foot a { color: var(--systemSecondary); font: var(--callout); }

.content { flex: 1; min-width: 0; padding: 28px var(--bodyGutter) 60px; }
.page-head { display: flex; align-items: end; justify-content: space-between; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.page-head h1 { font: var(--title-1-emphasized); }
.page-head .sub { font: var(--callout); color: var(--systemSecondary); margin-top: 4px; }
.page-head .actions { display: flex; gap: 8px; align-items: center; }

.topbar { display: none; }
@media (max-width: 700px) {
  .shell { display: block; }
  .sidebar { display: none; }
  .topbar {
    display: flex; align-items: center; gap: 4px;
    position: sticky; top: 0; z-index: 100; height: 44px;
    padding: 0 12px; overflow-x: auto;
    background: var(--pageFloor);
    border-bottom: var(--keyline);
  }
  .topbar .brand { font: var(--body-emphasized); margin-right: 8px; white-space: nowrap; }
  .topbar .nav-item { padding: 6px 10px; border-radius: 1000px; white-space: nowrap; }
}

/* ---- cards, stats, tables ---- */
.card {
  background: var(--pageRaised); border-radius: var(--radius-large);
  box-shadow: var(--shadow-small); margin-bottom: 24px;
}
@media (prefers-color-scheme: dark) { .card { background: var(--systemQuaternary); } }
.card .card-pad { padding: 20px; }
.card h3.card-title { font: var(--title-3-emphasized); padding: 16px 20px 0; }
.section-title { font: var(--title-2-emphasized); margin: 0 0 13px; }

.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 20px; margin-bottom: 24px; }
.stat-tile { background: var(--pageRaised); border-radius: var(--radius-large); box-shadow: var(--shadow-small); padding: 16px 20px; }
@media (prefers-color-scheme: dark) { .stat-tile { background: var(--systemQuaternary); } }
.stat-tile h4 { font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemSecondary); margin-bottom: 6px; }
.stat-tile .num { font: var(--title-1-emphasized); font-variant-numeric: tabular-nums; }
.stat-tile .num small { font: var(--callout); color: var(--systemTertiary); margin-left: 2px; }
.stat-tile .num.wrap { font: 500 13px/1.5 var(--font-mono); word-break: break-all; }

table.list { width: 100%; border-collapse: collapse; }
table.list th {
  font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemSecondary);
  text-align: left; padding: 12px 20px; border-bottom: var(--keyline);
}
table.list td { font: var(--body); padding: 11px 20px; border-bottom: var(--keyline); vertical-align: middle; }
table.list tr:last-child td { border-bottom: 0; }
table.list tbody tr { transition: background-color 175ms ease-in; }
table.list tbody tr:hover { background: var(--systemQuinary); }
table.list td.mono, .mono { font-family: var(--font-mono); font-size: 13px; }
table.list td.sec { color: var(--systemSecondary); }
.table-scroll { overflow-x: auto; }

.status { display: inline-flex; align-items: center; gap: 6px; font: var(--callout-emphasized); white-space: nowrap; }
.status .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--status-color, var(--systemGray)); }
.status.green  { --status-color: var(--systemGreen); }
.status.blue   { --status-color: var(--systemBlue); }
.status.orange { --status-color: var(--systemOrange); }
.status.red    { --status-color: var(--systemRed); }
.status.gray   { --status-color: var(--systemGray); }

.empty { padding: 36px 20px; text-align: center; font: var(--callout); color: var(--systemTertiary); }

pre.log-view {
  background: var(--systemGray6); border-radius: var(--radius-medium);
  padding: 16px; overflow: auto; max-height: 70vh;
  font: 400 12.5px/1.6 var(--font-mono); color: var(--systemPrimary);
  white-space: pre-wrap; word-break: break-word;
}

.builder-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 20px; margin-bottom: 24px; }
.builder-card { background: var(--pageRaised); border-radius: var(--radius-large); box-shadow: var(--shadow-small); padding: 16px 20px; }
@media (prefers-color-scheme: dark) { .builder-card { background: var(--systemQuaternary); } }
.builder-card h3 { font: var(--title-3-emphasized); margin-bottom: 2px; overflow: hidden; text-overflow: ellipsis; }
.builder-card .ep { font: var(--callout); color: var(--systemSecondary); margin-bottom: 10px; overflow: hidden; text-overflow: ellipsis; }
.builder-card .meta { display: flex; justify-content: space-between; font: var(--callout); color: var(--systemSecondary); padding-top: 8px; border-top: var(--keyline); }

.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 20px; }
@media (max-width: 800px) { .form-grid { grid-template-columns: 1fr; } }
.form-actions { display: flex; gap: 10px; align-items: center; margin-top: 8px; }
.save-msg { font: var(--callout); min-height: 15px; }
.save-msg.ok { color: var(--systemGreen); }
.save-msg.err { color: var(--systemRed); }

.docs-body { max-width: 720px; }
.docs-body h2 { font: var(--title-2-emphasized); margin: 26px 0 10px; }
.docs-body h2:first-child { margin-top: 0; }
.docs-body p { font: var(--body-tall); color: var(--systemSecondary); margin-bottom: 8px; text-wrap: pretty; }
.docs-body pre { font: 400 12.5px/1.6 var(--font-mono); color: var(--systemPrimary); background: var(--systemQuinary); border-radius: var(--radius-medium); padding: 12px 14px; margin: 8px 0 14px; overflow-x: auto; }

/* ---- build pipeline ---- */
.pipeline { display: flex; align-items: center; gap: 0; overflow-x: auto; padding: 4px 0; }
.pipe-stage { display: flex; align-items: center; flex-shrink: 0; }
.pipe-chip {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 7px 14px; border-radius: 1000px;
  font: var(--callout-emphasized); color: var(--systemSecondary);
  background: var(--systemQuinary); white-space: nowrap;
  transition: background-color 210ms ease-out;
}
.pipe-chip .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--systemGray); }
.pipe-stage.done .pipe-chip { color: var(--systemPrimary); }
.pipe-stage.done .pipe-chip .dot { background: var(--systemGreen); }
.pipe-stage.current .pipe-chip { background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .1)); color: var(--keyColor); }
.pipe-stage.current .pipe-chip .dot { background: var(--systemBlue); animation: pipe-pulse 1.2s ease-in-out infinite; }
.pipe-stage.failed .pipe-chip { background: rgba(255, 59, 48, .1); color: var(--systemRed); }
.pipe-stage.failed .pipe-chip .dot { background: var(--systemRed); }
@keyframes pipe-pulse { 0%,100% { opacity: 1; } 50% { opacity: .35; } }
.pipe-arrow { width: 22px; height: 1px; background: var(--labelDivider); margin: 0 2px; flex-shrink: 0; }
.log-filters { display: flex; gap: 6px; margin-bottom: 10px; flex-wrap: wrap; }
.log-filters .btn { padding: 4px 12px; font: var(--callout-emphasized); }
.log-filters .btn.active { background: var(--keyColor); color: hsla(0, 0%, 100%, .95); }

/* ---- settings sub-navigation ---- */
.settings-layout { display: flex; gap: 28px; align-items: flex-start; }
.subnav { width: 190px; flex-shrink: 0; display: flex; flex-direction: column; gap: 2px; position: sticky; top: 20px; }
.subnav a { display: block; padding: 8px 10px; border-radius: var(--radius-medium); font: var(--body); color: var(--systemPrimary); cursor: pointer; transition: background-color 175ms ease-in; }
.subnav a:hover { background: var(--systemQuinary); }
.subnav a.active { background: rgba(var(--keyColor-rgb), calc(var(--alpha-multiplier) * .08)); color: var(--keyColor); font: var(--body-emphasized); }
.subnav .subnav-label { font: var(--subhead-emphasized); text-transform: uppercase; color: var(--systemTertiary); padding: 12px 10px 4px; }
.settings-panels { flex: 1; min-width: 0; }
.panel { display: none; }
.panel.active { display: block; }
.settings-footer { position: sticky; bottom: 0; padding: 12px 0; background: var(--pageFloor); display: flex; gap: 10px; align-items: center; border-top: var(--keyline); }
@media (max-width: 800px) {
  .settings-layout { display: block; }
  .subnav { width: auto; flex-direction: row; overflow-x: auto; position: static; margin-bottom: 16px; }
  .subnav .subnav-label { display: none; }
  .subnav a { white-space: nowrap; }
}
.radio-row { display: flex; gap: 18px; margin: 2px 0 10px; flex-wrap: wrap; }
.radio-row label { display: flex; align-items: center; gap: 6px; font: var(--body); color: var(--systemPrimary); }
.card-actions { display: flex; gap: 10px; align-items: center; padding: 0 20px 16px; }
`

// i18nJS: English is the default (and the text baked into the HTML); Chinese
// is applied by replacing textContent of [data-i18n] nodes. Language comes
// from localStorage, falling back to the browser language.
const i18nJS = `
var I18N = {
  zh: {
    'nav.overview': '总览', 'nav.builds': '构建任务', 'nav.monitor': '构建节点',
    'nav.settings': '设置', 'nav.docs': '文档', 'nav.signout': '退出登录',
    'brand.sub': 'Gentoo Binhost 控制台',

    'title.landing': 'Portage Engine — 自托管 Gentoo 二进制包构建平台',
    'title.login': '登录 — Portage Engine',
    'title.overview': '总览 — Portage Engine',
    'title.builds': '构建任务 — Portage Engine',
    'title.detail': '构建详情 — Portage Engine',
    'title.logs': '构建日志 — Portage Engine',
    'title.monitor': '构建节点 — Portage Engine',
    'title.settings': '设置 — Portage Engine',
    'title.docs': '文档 — Portage Engine',

    'landing.signin': '登录控制台',
    'landing.eyebrow': 'Gentoo Binhost 构建平台',
    'landing.h1': '集中构建,处处安装',
    'landing.sub': '在 PVE 或云端按需拉起构建机,产物自动汇聚为 Portage 原生 binhost。客户端零改动,emerge 直接安装二进制包。',
    'landing.cta': '进入控制台',
    'landing.docs': '查看文档',
    'landing.f1.eyebrow': '按需构建机', 'landing.f1.title': '用完即毁的构建 VM',
    'landing.f1.text': '提交构建时在 Proxmox VE、GCP 或 AWS 自动创建临时虚拟机,按集群实时负载选择节点,构建完成即销毁。',
    'landing.f2.eyebrow': '原生 Binhost', 'landing.f2.title': '标准 Packages 索引',
    'landing.f2.text': '产物以 Portage 原生格式发布并支持 GPG 签名,任何 Gentoo 客户端配置一行 binrepos.conf 即可消费。',
    'landing.f3.eyebrow': '并行与汇聚', 'landing.f3.title': '多任务并行构建',
    'landing.f3.text': '多个构建任务各自独占虚拟机并行执行,产物统一回收进单一仓库,控制台实时跟踪每一步。',
    'landing.flow': '工作流',
    'landing.s1.t': '提交', 'landing.s1.d': '用客户端请求构建某个包,可附带完整 Portage 配置。',
    'landing.s2.t': '构建', 'landing.s2.d': '服务端拉起构建机、在容器中 emerge、回收产物并刷新索引。',
    'landing.s3.t': '消费', 'landing.s3.d': '任何 Gentoo 机器把本服务当 binhost,直接安装二进制包。',
    'landing.footer': 'Portage Engine · 自托管 Gentoo 二进制包构建平台',

    'login.h1': '登录', 'login.user': '用户名', 'login.pass': '密码',
    'login.submit': '登录', 'login.back': '返回首页',
    'login.badcreds': '用户名或密码错误', 'login.fail': '登录失败', 'login.neterr': '网络错误:',

    'common.refresh': '刷新', 'common.updated': '更新于 ',
    'common.loadfail': '加载失败:',
    'th.package': '包', 'th.version': '版本', 'th.arch': '架构', 'th.status': '状态',
    'th.jobid': '任务 ID', 'th.created': '创建时间', 'th.updated': '更新时间',

    'ov.h1': '总览', 'ov.recent': '最近构建',
    'ov.building': '构建中', 'ov.queued': '排队中', 'ov.instances': '云实例',
    'ov.total': '构建总数', 'ov.rate': '成功率',
    'ov.empty': '还没有构建任务。用 portage-client build 提交第一个吧。',

    'builds.h1': '构建任务', 'builds.count': '共 %d 个任务', 'builds.empty': '还没有构建任务。',

    'detail.h1': '构建详情', 'detail.logs': '查看日志', 'detail.error': '错误信息',
    'detail.livelog': '实时日志', 'detail.duration': '耗时',
    'detail.delete': '删除任务', 'detail.delete.confirm': '删除这条任务记录?',
    'detail.delete.fail': '删除失败:',
    'builds.cleanup': '清理失败任务', 'builds.cleanup.confirm': '移除所有失败的任务记录?',
    'pipe.queued': '排队', 'pipe.provision': '创建构建机', 'pipe.deploy': '部署 Builder',
    'pipe.build': '构建', 'pipe.collect': '回收产物', 'pipe.verify': '安装验证', 'pipe.cleanup': '释放实例',
    'filter.all': '全部', 'filter.provision': '供给', 'filter.deploy': '部署', 'filter.build': '构建',
    'filter.collect': '回收', 'filter.verify': '验证', 'filter.release': '释放',
    'detail.status': '状态', 'detail.arch': '架构', 'detail.created': '创建',
    'detail.updated': '更新', 'detail.instance': '实例', 'detail.artifact': '产物',
    'detail.unknown': '(未知)',

    'logs.h1': '构建日志', 'logs.back': '返回详情', 'logs.none': '(暂无日志)',
    'logs.fail': '日志加载失败:', 'logs.loading': '加载中…',

    'mon.h1': '构建节点', 'mon.sub': '静态 builder 与云实例',
    'mon.builders': 'Builder', 'mon.instances': '云实例',
    'mon.noBuilders': '没有已注册的 builder。静态 builder 需配置 SERVER_URL 后自动注册;云构建的临时实例不在此列。',
    'mon.noInstances': '当前没有运行中的云实例。',
    'mon.archLabel': '架构 ', 'mon.loadLabel': '负载 ',
    'mon.shell': '终端',
    'set.sec.upload': '产物上传',
    'set.upload.desc': '配置后,新构建的二进制包(连同 Packages 索引与签名公钥)会推送到内网镜像站的制品接口,安装验证也会改用镜像站 URL。',
    'set.upload.url': '镜像站地址', 'set.upload.url.hint': '留空则不上传,包仅由本服务的 /binpkgs 提供',
    'set.upload.dir': '制品目录', 'set.upload.dir.hint': '文件位于 /local/<目录>/… 下,该 URL 即为内网 binhost',
    'set.upload.user': '用户名', 'set.upload.pass': '密码',
    'detail.artifact.deps': '个依赖包',
    'title.shell': '终端 — Portage Engine',
    'shell.back': '返回', 'shell.title': '实例终端',
    'shell.connected': '已连接', 'shell.closed': '已断开', 'shell.error': '连接错误',
    'th.instance': '实例', 'th.provider': '提供商', 'th.ip': 'IP',

    'set.h1': '设置', 'set.sub': '云构建配置在此管理,保存后立即生效并覆盖 server.conf',
    'set.cat.general': '通用', 'set.cat.infra': '基础设施', 'set.cat.access': '接入',
    'set.sec.mirrors': '镜像加速', 'set.sec.buildconf': '构建配置',
    'set.mirrors.hint': '拉起构建机时使用的内网镜像——局域网内可大幅加速部署。全部可选。',
    'set.mirrors.apt': 'APT 镜像(基础 URL)',
    'set.mirrors.apt.hint': '需提供 /debian 与 /debian-security;构建机的 sources.list 会重写指向它',
    'set.mirrors.dockerdl': 'Docker 下载镜像',
    'set.mirrors.dockerdl.hint': 'download.docker.com 的镜像;通过 DOWNLOAD_URL 传给内置安装脚本',
    'set.mirrors.dockerreg': 'Docker Registry 镜像',
    'set.mirrors.dockerreg.hint': 'Docker Hub 镜像,写入 daemon.json;加速 gentoo/stage3 拉取',
    'set.mirrors.gentoo': 'Gentoo 镜像(GENTOO_MIRRORS)',
    'set.mirrors.gentoo.hint': 'distfiles 与 webrsync 快照;写入构建机的 make.conf',
    'set.mirrors.method': 'Portage 树同步方式',
    'set.mirrors.method.hint': 'webrsync 从 Gentoo 镜像下载一个快照包;rsync 逐文件同步,没有局域网 rsync 镜像时很慢',
    'set.mirrors.sync': 'Portage 同步 URI(可选)',
    'set.mirrors.sync.hint': '自定义 repos.conf sync-uri;留空则用 Gentoo 镜像的 webrsync 快照',
    'set.makeconf': '附加 make.conf 内容',
    'set.makeconf.hint': '逐字追加到每台构建机生成的 make.conf(全局 USE、ACCEPT_LICENSE、FEATURES、EMERGE_DEFAULT_OPTS 等)。包级 USE 由客户端配置包传递。',
    'set.buildfeatures': '构建容器 FEATURES',
    'set.buildfeatures.hint': '追加到构建容器的 FEATURES。Docker 构建需要 "-userpriv -usersandbox"(容器内无法 unshare/降权,签名校验须以 root 运行)。仅当在完整 Gentoo 虚拟机中构建时才留空。',
    'set.buildmode': '构建模式',
    'set.buildmode.hint': 'Docker:在 Debian 虚拟机上的 gentoo/stage3 容器里构建。原生 Gentoo 虚拟机:用 UEFI cloud-init 模板拉起 Gentoo 虚拟机原生 emerge——emerge 内签名只有这个模式能正常工作。原生模式下请把 PVE 模板设为你的 Gentoo 模板。',
    'set.signbinpkgs': '签名二进制包(emerge 内)',
    'set.signbinpkgs.hint': '在 emerge 期间启用 gpkg 签名。Portage 的签名后自校验需要 getuto 的 CA 证书为该密钥背书,在 Docker 构建容器内无法做到——仅在原生 Gentoo 虚拟机构建节点上启用。关闭时构建为未签名包;公钥仍会发布,安装验证仍会导入它。',
    'set.sec.general': '后端与测试', 'set.sec.builders': '静态 Builder',
    'set.sec.ssh': 'SSH 密钥', 'set.sec.net': '网络与投递', 'set.sec.gpg': 'GPG 签名',
    'set.gpg.state': '签名状态', 'set.gpg.keyid': '密钥 ID',
    'set.gpg.on': '已启用', 'set.gpg.off': '未启用',
    'set.gpg.name': '密钥名称', 'set.gpg.email': '密钥邮箱',
    'set.gpg.generate': '生成密钥并启用签名',
    'set.gpg.working': '正在生成密钥…', 'set.gpg.done': '签名已启用,密钥 ', 'set.gpg.fail': '失败:',
    'set.gpg.hint': '在服务端创建签名密钥(已配置则直接采用)并启用签名。客户端从 /api/v1/gpg/public-key 获取公钥;portage-client configure 会自动配置 verify-signature。',
    'set.gpg.pubkey': '公钥', 'set.gpg.download': '下载公钥',
    'set.conn': '连接', 'set.placement': '节点调度', 'set.resources': '资源',
    'set.pveuser': '用户名(Token 的替代方式)',
    'set.pveuser.hint': 'user@realm 格式;两者都填时优先使用 Token',
    'set.pvepass': '密码',
    'set.place.auto': '自动调度(推荐)', 'set.place.manual': '指定节点',
    'set.place.auto.hint': '每次构建按集群实时负载自动落在最空闲的可用节点上',
    'set.provider.hint': '未配置静态 Builder 时使用;后续可扩展更多后端',
    'set.testbuild': '测试构建',
    'set.testbuild.hint': '用当前设置走完整流程:拉起构建机 → emerge 编译 → 产物回收进 binhost。',
    'set.testbuild.pkg': '包名(atom)',
    'set.testbuild.go': '发起测试构建',
    'set.testbuild.saving': '正在保存设置…',
    'set.testbuild.submitting': '正在提交构建…',
    'set.testbuild.submitted': '已提交,任务 ',
    'set.testbuild.view': '查看构建详情',
    'set.testbuild.fail': '测试构建失败:',
    'set.backend': '构建后端', 'set.provider': '默认提供商', 'set.ttl': '实例 TTL(分钟)',
    'set.ttl.hint': '闲置实例会被新构建复用,超过该闲置时长自动销毁;0 为不限制',
    'set.ttl': '实例闲置 TTL(分钟)',
    'set.verify': '每个 binpkg 构建后在全新容器中从 binhost 安装验证,通过才算成功(推荐)',
    'set.dockerimage': '构建容器镜像',
    'set.dockerimage.hint': '新实例每次拉取;Docker Hub 镜像慢时可指向本地 registry 中的镜像(如 hub.infra.plz.ac/gentoo/stage3:latest)',
    'set.builders': '静态 Builder(逗号分隔 URL)',
    'set.builders.hint': '配置后构建轮询分发到这些 builder;留空则每次构建按需拉起云端临时 VM',
    'set.gcp': 'Google Cloud',
    'set.gcp.project': '项目', 'set.gcp.region': '区域', 'set.gcp.zone': '可用区',
    'set.gcp.keyfile': '服务账号密钥文件(服务端路径)',
    'set.aws': 'AWS',
    'set.aws.ak': 'Access Key ID', 'set.aws.sk': 'Secret Access Key',
    'set.pve': 'Proxmox VE',
    'set.endpoint': 'API 地址', 'set.node': '目标节点',
    'set.node.hint': '填 auto 时按集群实时负载自动选最空闲节点',
    'set.nodes': '候选节点(可选,逗号分隔)',
    'set.nodes.hint': '共享存储集群配合 auto 使用;留空则仅在持有模板的节点中选',
    'set.template': 'VM 模板',
    'set.template.hint': '须为装有 cloud-init 与 qemu-guest-agent 的 QEMU 模板',
    'set.nameserver': '构建机 DNS(可选)',
    'set.nameserver.hint': '经 cloud-init 下发;填内网 DNS 让构建机能解析镜像站/registry 域名',
    'set.cicustom': 'cloud-init snippet(cicustom,可选)',
    'set.cicustom.hint': '克隆后保留;基础镜像没有 qemu-guest-agent 时用 vendor snippet 首启安装',
    'set.tokenid': 'API Token ID', 'set.secret': 'API Token Secret',
    'set.secret.saved': '已保存;留空表示保持不变', 'set.secret.unset': '尚未设置',
    'set.secret.ph': '••••••••(留空保持不变)',
    'set.storage': '存储池', 'set.bridge': '网桥',
    'set.tls': '跳过 TLS 证书校验(自签证书;更安全的做法是把 PVE CA 加入系统信任库)',
    'set.ssh': 'SSH 部署',
    'set.keypath': '私钥路径', 'set.keypath.hint': '对应 .pub 公钥经 cloud-init 注入新 VM',
    'set.sshuser': 'SSH 用户', 'set.sshuser.hint': '部署脚本需要 root 权限',
    'set.knownhosts': 'known_hosts 路径(可选)',
    'set.hostkey': '跳过 SSH host key 校验(新建 VM 首连必需,或改用上方 known_hosts)',
    'set.net': '网络与投递',
    'set.callback': '回调地址', 'set.callback.hint': '构建 VM 访问本服务的地址,须为 VM 可达的 IP',
    'set.binpath': 'Builder 二进制路径', 'set.binpath.hint': '部署时 scp 到实例;需为 linux 且架构匹配',
    'set.binurl': 'Builder 二进制 URL(可选)', 'set.binurl.hint': '实例启动时自行下载;与路径同时设置时路径优先',
    'set.save': '保存', 'set.test': '测试 PVE 连接',
    'set.saving': '保存中…', 'set.saved': '已保存,立即生效', 'set.savefail': '保存失败:',
    'set.loadfail': '设置加载失败:',
    'set.testing': '正在连接 PVE…', 'set.testfail': '连接失败:',
    'set.testok': '连接成功,发现 %d 个节点',
    'set.clusternodes': '集群节点',
    'th.node': '节点', 'th.freemem': '空闲内存', 'th.cpu': 'CPU 负载', 'th.hastpl': '持有模板',
    'set.yes': '是', 'set.no': '否',

    'docs.h1': '文档', 'docs.sub': '快速上手',
    'docs.consume': '消费二进制包',
    'docs.consume.p': '在任何 Gentoo 机器上把本服务配置为 binhost,之后 emerge 自动拉取并校验二进制包,缺包时回退本地编译:',
    'docs.build': '请求构建',
    'docs.build.p': 'Portage 没有请求远端构建的原生机制,用客户端提交(可选 -wait 等待完成):',
    'docs.build.p2': '提交后服务端在云端拉起临时构建机(或分发给静态 builder),产物自动进入 binhost 仓库并刷新 Packages 索引。',
    'docs.cloud': '云构建配置',
    'docs.cloud.p': 'PVE / 云提供商的接入参数在「设置」页管理,保存即生效。PVE 模板制作与端到端测试步骤见仓库内 docs/PVE_TESTING.md。',
    'docs.gpg': 'GPG 验证',
    'docs.gpg.p': '启用签名后,客户端 configure 时会自动配置 verify-signature;公钥由服务端 /api/v1/gpg/public-key 分发。',

    'st.queued': '排队中', 'st.claimed': '已认领', 'st.provisioning': '开机中',
    'st.forwarding': '分发中', 'st.deploying': '部署中', 'st.building': '构建中', 'st.verifying': '验证中', 'st.success': '成功',
    'st.completed': '完成', 'st.failed': '失败', 'st.online': '在线',
    'st.offline': '离线', 'st.running': '运行中', 'st.destroy_failed': '销毁失败'
  }
};
function peLang() {
  var saved = null;
  try { saved = localStorage.getItem('pe_lang'); } catch (e) {}
  if (saved === 'zh' || saved === 'en') return saved;
  return (navigator.language || '').toLowerCase().indexOf('zh') === 0 ? 'zh' : 'en';
}
function t(key, fallback) {
  var lang = peLang();
  if (lang !== 'en' && I18N[lang] && Object.prototype.hasOwnProperty.call(I18N[lang], key)) return I18N[lang][key];
  return fallback !== undefined ? fallback : key;
}
function applyI18n() {
  var lang = peLang();
  document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
  var nodes = document.querySelectorAll('[data-i18n]');
  for (var i = 0; i < nodes.length; i++) {
    var n = nodes[i], key = n.getAttribute('data-i18n');
    if (!n.hasAttribute('data-i18n-default')) n.setAttribute('data-i18n-default', n.textContent);
    n.textContent = lang === 'en' ? n.getAttribute('data-i18n-default') : t(key, n.getAttribute('data-i18n-default'));
  }
  var toggles = document.querySelectorAll('.lang-btn');
  for (var j = 0; j < toggles.length; j++) toggles[j].textContent = lang === 'zh' ? 'English' : '中文';
}
function initLangToggles() {
  var toggles = document.querySelectorAll('.lang-btn');
  for (var i = 0; i < toggles.length; i++) {
    toggles[i].addEventListener('click', function () {
      try { localStorage.setItem('pe_lang', peLang() === 'zh' ? 'en' : 'zh'); } catch (e) {}
      applyI18n();
      if (typeof onLangChange === 'function') onLangChange();
    });
  }
}
applyI18n();
document.addEventListener('DOMContentLoaded', function () { applyI18n(); initLangToggles(); });
`

// baseJS: shared, injected into every shell page. DOM building goes through
// el()/textContent only — API data never reaches innerHTML.
const baseJS = `
function el(tag, cls, text) {
  var n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined && text !== null) n.textContent = String(text);
  return n;
}
function clear(node) { while (node.firstChild) node.removeChild(node.firstChild); }
async function api(path, opts) {
  var r = await fetch(path, opts);
  if (r.status === 401) { location.href = '/login'; throw new Error('unauthorized'); }
  if (!r.ok) {
    var msg = 'HTTP ' + r.status;
    try { var b = await r.json(); if (b && (b.details || b.error)) msg = b.details || b.error; } catch (e) {}
    throw new Error(msg);
  }
  return r.json();
}
function fmtTime(s) {
  if (!s) return '-';
  var d = new Date(s);
  return isNaN(d) ? String(s) : d.toLocaleString();
}
var STATUS_COLORS = {
  queued: 'gray', claimed: 'orange', provisioning: 'orange', forwarding: 'orange',
  deploying: 'orange', verifying: 'blue',
  building: 'blue', success: 'green', completed: 'green', failed: 'red',
  online: 'green', offline: 'red', running: 'green', destroy_failed: 'red'
};
function statusBadge(s) {
  var color = STATUS_COLORS[s] || 'gray';
  var wrap = el('span', 'status ' + color);
  wrap.appendChild(el('span', 'dot'));
  wrap.appendChild(el('span', null, t('st.' + s, s || '-')));
  return wrap;
}
function showError(containerId, err) {
  var c = document.getElementById(containerId);
  if (!c) return;
  clear(c);
  c.appendChild(el('div', 'empty', t('common.loadfail', 'Failed to load: ') + err.message));
}
`

// appPage assembles a full authed page: shared chrome + per-page content and
// script. active marks the nav item; titleKey is the i18n key for <title>.
func appPage(titleEN, titleKey, active, content, script string) string {
	nav := ""
	for _, it := range [][3]string{
		{"/overview", "Overview", "nav.overview"},
		{"/builds", "Builds", "nav.builds"},
		{"/monitor", "Build Nodes", "nav.monitor"},
		{"/settings", "Settings", "nav.settings"},
		{"/docs", "Docs", "nav.docs"},
	} {
		cls := "nav-item"
		if it[0] == "/"+active {
			cls += " active"
		}
		nav += `<a class="` + cls + `" href="` + it[0] + `" data-i18n="` + it[2] + `">` + it[1] + `</a>`
	}

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title data-i18n="` + titleKey + `">` + titleEN + ` — Portage Engine</title>
<link rel="stylesheet" href="/static/apple.css">
</head>
<body>
<div class="topbar"><span class="brand">Portage Engine</span>` + nav + `</div>
<div class="shell">
  <nav class="sidebar" aria-label="Main navigation">
    <div class="brand">Portage Engine<span data-i18n="brand.sub">Gentoo binhost console</span></div>
    ` + nav + `
    <div class="spacer"></div>
    <div class="foot"><button class="lang-btn" type="button">中文</button></div>
    {{if .AuthEnabled}}<div class="foot"><a href="/logout" data-i18n="nav.signout">Sign Out</a></div>{{end}}
    <div class="foot">binhost:<span class="mono"> /binpkgs</span></div>
  </nav>
  <main class="content">
` + content + `
  </main>
</div>
<script>` + i18nJS + baseJS + script + `</script>
</body>
</html>`
}

// ---------------------------------------------------------------------------
// Landing (public)
// ---------------------------------------------------------------------------

const landingHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title data-i18n="title.landing">Portage Engine — self-hosted Gentoo binary package platform</title>
<link rel="stylesheet" href="/static/apple.css">
</head>
<body>
<nav class="landing-nav">
  <span class="brand">Portage Engine</span>
  <span class="side">
    <button class="lang-btn" type="button">中文</button>
    <a class="btn" href="/overview" data-i18n="landing.signin">Sign In</a>
  </span>
</nav>
<header class="landing-hero">
  <p class="eyebrow" data-i18n="landing.eyebrow">Gentoo binhost platform</p>
  <h1 data-i18n="landing.h1">Build once, install everywhere</h1>
  <p class="sub" data-i18n="landing.sub">Spin up build machines on demand on Proxmox VE or in the cloud. Artifacts converge into a native Portage binhost — clients install binary packages with plain emerge, no changes required.</p>
  <div class="cta">
    <a class="btn blue" href="/overview" data-i18n="landing.cta">Open Console</a>
    <a class="btn" href="/docs" data-i18n="landing.docs">Documentation</a>
  </div>
</header>
<section class="landing-grid" aria-label="Features">
  <article class="landing-card">
    <h4 data-i18n="landing.f1.eyebrow">On-demand builders</h4>
    <h3 data-i18n="landing.f1.title">Ephemeral build VMs</h3>
    <p data-i18n="landing.f1.text">Each build gets a fresh VM on Proxmox VE, GCP, or AWS — placed on the least-loaded node and destroyed when the build finishes.</p>
  </article>
  <article class="landing-card">
    <h4 data-i18n="landing.f2.eyebrow">Native binhost</h4>
    <h3 data-i18n="landing.f2.title">Standard Packages index</h3>
    <p data-i18n="landing.f2.text">Artifacts are published in Portage's native format with GPG signing. Any Gentoo client consumes them with one line of binrepos.conf.</p>
  </article>
  <article class="landing-card">
    <h4 data-i18n="landing.f3.eyebrow">Parallel and converged</h4>
    <h3 data-i18n="landing.f3.title">Concurrent builds</h3>
    <p data-i18n="landing.f3.text">Builds run in parallel, each on its own VM. Artifacts converge into a single repository, tracked live from the console.</p>
  </article>
</section>
<section class="landing-flow" aria-label="Workflow">
  <h2 data-i18n="landing.flow">Workflow</h2>
  <div class="steps">
    <div class="step">
      <h4 data-i18n="landing.s1.t">Submit</h4>
      <p data-i18n="landing.s1.d">Request a package build from the client, optionally with your full Portage configuration.</p>
      <span class="mono">portage-client build -package app-misc/jq</span>
    </div>
    <div class="step">
      <h4 data-i18n="landing.s2.t">Build</h4>
      <p data-i18n="landing.s2.d">The server provisions a builder, runs emerge in a container, collects the artifact, and refreshes the index.</p>
      <span class="mono">provision &rarr; emerge &rarr; collect &rarr; index</span>
    </div>
    <div class="step">
      <h4 data-i18n="landing.s3.t">Consume</h4>
      <p data-i18n="landing.s3.d">Any Gentoo machine points at this server as its binhost and installs binaries directly.</p>
      <span class="mono">emerge --getbinpkg app-misc/jq</span>
    </div>
  </div>
</section>
<footer class="landing-footer" data-i18n="landing.footer">Portage Engine · self-hosted Gentoo binary package platform</footer>
<script>` + i18nJS + `</script>
</body>
</html>`

// ---------------------------------------------------------------------------
// Login (public)
// ---------------------------------------------------------------------------

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title data-i18n="title.login">Sign In — Portage Engine</title>
<link rel="stylesheet" href="/static/apple.css">
</head>
<body>
<div class="auth-wrap">
  <form class="auth-card" id="login-form">
    <p class="brand">Portage Engine</p>
    <h1 data-i18n="login.h1">Sign In</h1>
    <div class="field">
      <label for="u" data-i18n="login.user">Username</label>
      <input type="text" id="u" autocomplete="username" autocapitalize="off" autocorrect="off" spellcheck="false">
    </div>
    <div class="field">
      <label for="p" data-i18n="login.pass">Password</label>
      <input type="password" id="p" autocomplete="current-password">
    </div>
    <p class="auth-err" id="err" role="status"></p>
    <button class="btn blue" type="submit" data-i18n="login.submit">Sign In</button>
    <p class="auth-note"><a href="/" data-i18n="login.back">Back to home</a><button class="lang-btn" type="button">中文</button></p>
  </form>
</div>
<script>` + i18nJS + `
document.getElementById('login-form').addEventListener('submit', async function (e) {
  e.preventDefault();
  var err = document.getElementById('err');
  err.textContent = '';
  try {
    var r = await fetch('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: document.getElementById('u').value,
        password: document.getElementById('p').value
      })
    });
    if (!r.ok) {
      err.textContent = r.status === 401 ? t('login.badcreds', 'Wrong username or password')
                                         : t('login.fail', 'Sign-in failed') + ' (HTTP ' + r.status + ')';
      return;
    }
    location.href = '/overview';
  } catch (ex) { err.textContent = t('login.neterr', 'Network error: ') + ex.message; }
});
</script>
</body>
</html>`

// ---------------------------------------------------------------------------
// Overview
// ---------------------------------------------------------------------------

const overviewContent = `
<div class="page-head">
  <div><h1 data-i18n="ov.h1">Overview</h1><p class="sub" id="updated"></p></div>
  <div class="actions"><button class="btn" id="refresh" data-i18n="common.refresh">Refresh</button></div>
</div>
<div class="stat-grid" id="stats"></div>
<h2 class="section-title" data-i18n="ov.recent">Recent Builds</h2>
<div class="card">
  <div class="table-scroll"><table class="list" aria-label="Recent builds">
    <thead><tr>
      <th data-i18n="th.package">Package</th><th data-i18n="th.version">Version</th>
      <th data-i18n="th.arch">Arch</th><th data-i18n="th.status">Status</th>
      <th data-i18n="th.updated">Updated</th>
    </tr></thead>
    <tbody id="recent"></tbody>
  </table></div>
  <div id="recent-empty"></div>
</div>`

const overviewJS = `
function statTile(labelKey, labelEN, value, suffix) {
  var tle = el('div', 'stat-tile');
  tle.appendChild(el('h4', null, t(labelKey, labelEN)));
  var n = el('div', 'num', value);
  if (suffix) n.appendChild(el('small', null, suffix));
  tle.appendChild(n);
  return tle;
}
async function load() {
  try {
    var s = await api('/api/status');
    var g = document.getElementById('stats');
    clear(g);
    g.appendChild(statTile('ov.building', 'Building', s.active_builds || 0));
    g.appendChild(statTile('ov.queued', 'Queued', s.queued_builds || 0));
    g.appendChild(statTile('ov.instances', 'Cloud Instances', s.active_instances || 0));
    g.appendChild(statTile('ov.total', 'Total Builds', s.total_builds || 0));
    g.appendChild(statTile('ov.rate', 'Success Rate', (s.success_rate || 0).toFixed(1), '%'));
    document.getElementById('updated').textContent = t('common.updated', 'Updated ') + fmtTime(s.last_updated);
  } catch (e) { showError('stats', e); }
  try {
    var builds = await api('/api/builds?limit=10');
    if (!Array.isArray(builds)) builds = builds.builds || [];
    var tb = document.getElementById('recent');
    var emptyBox = document.getElementById('recent-empty');
    clear(tb); clear(emptyBox);
    if (!builds.length) { emptyBox.appendChild(el('div', 'empty', t('ov.empty', 'No builds yet. Submit one with portage-client build.'))); return; }
    builds.forEach(function (b) {
      var tr = el('tr');
      var pkg = el('td');
      var a = el('a', null, b.package_name || t('detail.unknown', '(unknown)'));
      a.href = '/build/' + encodeURIComponent(b.job_id);
      pkg.appendChild(a);
      tr.appendChild(pkg);
      tr.appendChild(el('td', 'sec', b.version || '-'));
      tr.appendChild(el('td', 'sec', b.arch || '-'));
      var st = el('td'); st.appendChild(statusBadge(b.status)); tr.appendChild(st);
      tr.appendChild(el('td', 'sec', fmtTime(b.updated_at)));
      tb.appendChild(tr);
    });
  } catch (e) { showError('recent-empty', e); }
}
function onLangChange() { load(); }
document.getElementById('refresh').addEventListener('click', load);
load();
setInterval(load, 15000);
`

// ---------------------------------------------------------------------------
// Builds list
// ---------------------------------------------------------------------------

const buildsContent = `
<div class="page-head">
  <div><h1 data-i18n="builds.h1">Builds</h1><p class="sub" id="count"></p></div>
  <div class="actions">
    <button class="btn" id="cleanup-failed" data-i18n="builds.cleanup">Clean Up Failed</button>
    <button class="btn" id="refresh" data-i18n="common.refresh">Refresh</button>
  </div>
</div>
<div class="card">
  <div class="table-scroll"><table class="list" aria-label="Builds">
    <thead><tr>
      <th data-i18n="th.package">Package</th><th data-i18n="th.version">Version</th>
      <th data-i18n="th.arch">Arch</th><th data-i18n="th.status">Status</th>
      <th data-i18n="th.jobid">Job ID</th><th data-i18n="th.created">Created</th>
      <th data-i18n="th.updated">Updated</th>
    </tr></thead>
    <tbody id="rows"></tbody>
  </table></div>
  <div id="empty"></div>
</div>`

const buildsJS = `
async function load() {
  try {
    var builds = await api('/api/builds');
    if (!Array.isArray(builds)) builds = builds.builds || [];
    document.getElementById('count').textContent = t('builds.count', '%d jobs total').replace('%d', builds.length);
    var tb = document.getElementById('rows');
    var emptyBox = document.getElementById('empty');
    clear(tb); clear(emptyBox);
    if (!builds.length) { emptyBox.appendChild(el('div', 'empty', t('builds.empty', 'No builds yet.'))); return; }
    builds.forEach(function (b) {
      var tr = el('tr');
      var pkg = el('td');
      var a = el('a', null, b.package_name || t('detail.unknown', '(unknown)'));
      a.href = '/build/' + encodeURIComponent(b.job_id);
      pkg.appendChild(a);
      tr.appendChild(pkg);
      tr.appendChild(el('td', 'sec', b.version || '-'));
      tr.appendChild(el('td', 'sec', b.arch || '-'));
      var st = el('td'); st.appendChild(statusBadge(b.status)); tr.appendChild(st);
      var idTd = el('td', 'mono sec', (b.job_id || '').slice(0, 8));
      idTd.title = b.job_id || '';
      tr.appendChild(idTd);
      tr.appendChild(el('td', 'sec', fmtTime(b.created_at)));
      tr.appendChild(el('td', 'sec', fmtTime(b.updated_at)));
      tb.appendChild(tr);
    });
  } catch (e) { showError('empty', e); }
}
function onLangChange() { load(); }
document.getElementById('cleanup-failed').addEventListener('click', async function () {
  if (!confirm(t('builds.cleanup.confirm', 'Remove all failed job records?'))) return;
  try {
    var r = await api('/api/builds/cleanup-failed', { method: 'POST' });
    load();
  } catch (e) { alert(t('detail.delete.fail', 'Delete failed: ') + e.message); }
});
document.getElementById('refresh').addEventListener('click', load);
load();
setInterval(load, 15000);
`

// ---------------------------------------------------------------------------
// Build detail
// ---------------------------------------------------------------------------

const buildDetailContent = `
<div class="page-head" data-job-id="{{.JobID}}" id="head">
  <div><h1 id="title" data-i18n="detail.h1">Build Details</h1><p class="sub mono" id="jid"></p></div>
  <div class="actions">
    <a class="btn" id="logs-link" href="#" data-i18n="detail.logs">View Logs</a>
    <button class="btn" id="delete-job" style="display:none" data-i18n="detail.delete">Delete Job</button>
    <button class="btn" id="refresh" data-i18n="common.refresh">Refresh</button>
  </div>
</div>
<div class="card"><div class="card-pad">
  <div class="pipeline" id="pipeline" aria-label="Build pipeline"></div>
</div></div>
<div class="stat-grid" id="meta"></div>
<div class="card" id="err-card" style="display:none">
  <h3 class="card-title" data-i18n="detail.error">Error</h3>
  <div class="card-pad"><pre class="log-view" id="err-text"></pre></div>
</div>
<div class="card">
  <h3 class="card-title" data-i18n="detail.livelog">Live Log</h3>
  <div class="card-pad">
    <div class="log-filters" id="log-filters"></div>
    <pre class="log-view" id="live-log">…</pre>
  </div>
</div>`

const buildDetailJS = `
var jobID = document.getElementById('head').getAttribute('data-job-id');
document.getElementById('jid').textContent = jobID;
document.getElementById('logs-link').href = '/logs/' + encodeURIComponent(jobID);
var lastDetail = null;
function fmtDuration(ms) {
  if (ms < 0) ms = 0;
  var s = Math.floor(ms / 1000);
  var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  return (h ? h + 'h ' : '') + (h || m ? m + 'm ' : '') + sec + 's';
}
function durationTile(b) {
  var terminal = b.status === 'failed' || b.status === 'completed' || b.status === 'success';
  var end = terminal ? new Date(b.updated_at) : new Date();
  var tle = el('div', 'stat-tile');
  tle.appendChild(el('h4', null, t('detail.duration', 'Duration')));
  var v = el('div', 'num', fmtDuration(end - new Date(b.created_at)));
  v.id = 'duration-num';
  v.style.font = 'var(--title-3-emphasized)';
  v.style.fontVariantNumeric = 'tabular-nums';
  tle.appendChild(v);
  return tle;
}
setInterval(function () {
  var n = document.getElementById('duration-num');
  if (!n || !lastDetail) return;
  var b = lastDetail;
  var terminal = b.status === 'failed' || b.status === 'completed' || b.status === 'success';
  if (!terminal) n.textContent = fmtDuration(new Date() - new Date(b.created_at));
}, 1000);
function metaTile(labelKey, labelEN, node, wrap) {
  var tle = el('div', 'stat-tile');
  tle.appendChild(el('h4', null, t(labelKey, labelEN)));
  var v = el('div', wrap ? 'num wrap' : 'num');
  if (!wrap) v.style.font = 'var(--title-3-emphasized)';
  if (typeof node === 'string') v.textContent = node; else v.appendChild(node);
  tle.appendChild(v);
  return tle;
}
function basename(p) { var i = (p || '').lastIndexOf('/'); return i >= 0 ? p.slice(i + 1) : p; }
(function () {
  var st = document.createElement('style');
  st.textContent = '.artifact-extra{margin-top:4px;font-size:11px;opacity:.85}.artifact-extra a{color:var(--keyColor)}.artifact-extra-note{margin-top:4px;font-size:11px;color:var(--systemSecondary)}';
  document.head.appendChild(st);
})();
async function load() {
  try {
    var b = await api('/api/builds/detail?job_id=' + encodeURIComponent(jobID));
    if (b.package_name) document.getElementById('title').textContent = b.package_name + (b.version ? ' ' + b.version : '');
    var g = document.getElementById('meta');
    clear(g);
    g.appendChild(metaTile('detail.status', 'Status', statusBadge(b.status)));
    g.appendChild(metaTile('detail.arch', 'Arch', b.arch || '-'));
    g.appendChild(metaTile('detail.created', 'Created', fmtTime(b.created_at)));
    g.appendChild(metaTile('detail.updated', 'Updated', fmtTime(b.updated_at)));
    lastDetail = b;
    g.appendChild(durationTile(b));
    if (b.instance_id) g.appendChild(metaTile('detail.instance', 'Instance', b.instance_id, true));
    if (b.artifact_url) {
      var wrap = el('div');
      var a = el('a', null, basename(b.artifact_url));
      a.href = b.artifact_url;
      a.setAttribute('download', '');
      wrap.appendChild(a);
      var extras = (b.artifacts || []).filter(function (u) { return u !== b.artifact_url; });
      extras.forEach(function (u) {
        var row = el('div', 'artifact-extra');
        var ea = el('a', null, basename(u));
        ea.href = u;
        ea.setAttribute('download', '');
        row.appendChild(ea);
        wrap.appendChild(row);
      });
      if (extras.length) {
        var note = el('div', 'artifact-extra-note', '+' + extras.length + ' ' + t('detail.artifact.deps', 'dependency package(s)'));
        wrap.appendChild(note);
      }
      g.appendChild(metaTile('detail.artifact', 'Artifact', wrap, true));
    } else if (b.artifact_path) {
      g.appendChild(metaTile('detail.artifact', 'Artifact', basename(b.artifact_path), true));
    }
    var delBtn = document.getElementById('delete-job');
    var terminal = b.status === 'failed' || b.status === 'completed' || b.status === 'success';
    delBtn.style.display = terminal ? '' : 'none';
    var errCard = document.getElementById('err-card');
    if (b.error) { errCard.style.display = ''; document.getElementById('err-text').textContent = b.error; }
    else errCard.style.display = 'none';
  } catch (e) { showError('meta', e); }
}
function onLangChange() { load(); renderLogs(); }

var STAGES = [
  { key: 'queued',    en: 'Queued' },
  { key: 'provision', en: 'Provision VM', marker: '[provision]' },
  { key: 'deploy',    en: 'Deploy Builder', marker: '[deploy]' },
  { key: 'build',     en: 'Build', marker: '[build] submitting' },
  { key: 'collect',   en: 'Collect Artifact', marker: '[collect]' },
  { key: 'verify',    en: 'Verify Install', marker: '[verify]' },
  { key: 'cleanup',   en: 'Release', marker: '[cleanup]' }
];
var FILTERS = [
  { key: 'all',       en: 'All',       prefixes: null },
  { key: 'provision', en: 'Provision', prefixes: ['[provision]', '[terraform]'] },
  { key: 'deploy',    en: 'Deploy',    prefixes: ['[deploy]', '[remote]'] },
  { key: 'build',     en: 'Build',     prefixes: ['[build]'] },
  { key: 'collect',   en: 'Collect',   prefixes: ['[collect]'] },
  { key: 'verify',    en: 'Verify',    prefixes: ['[verify]'] },
  { key: 'release',   en: 'Release',   prefixes: ['[cleanup]'] }
];
var activeFilter = 'all';
var lastLogText = '';

function stageState(idx, reachedIdx, status, failedIdx, cleanupDone) {
  var terminal = status === 'completed' || status === 'success';
  if (terminal) return 'done';
  if (status === 'failed') {
    if (failedIdx >= 0) {
      if (idx === failedIdx) return 'failed';
      if (idx < failedIdx) return 'done';
      // The release stage still runs after a failure.
      if (STAGES[idx].key === 'cleanup' && cleanupDone) return 'done';
      return 'pending';
    }
    return idx === reachedIdx ? 'failed' : (idx < reachedIdx ? 'done' : 'pending');
  }
  if (idx < reachedIdx) return 'done';
  if (idx === reachedIdx) return 'current';
  return 'pending';
}
var STATUS_STAGE = { queued: 0, claimed: 0, provisioning: 1, deploying: 2, forwarding: 3, building: 3, verifying: 5 };
function renderPipeline(logText, status, failedStage) {
  var reached = 0;
  for (var i = 0; i < STAGES.length; i++) {
    if (STAGES[i].marker && logText.indexOf(STAGES[i].marker) !== -1) reached = i;
  }
  // Status is authoritative when it maps further than the (possibly truncated)
  // log markers.
  if (STATUS_STAGE[status] !== undefined && STATUS_STAGE[status] > reached) reached = STATUS_STAGE[status];
  if (status === 'completed' || status === 'success') reached = STAGES.length - 1;
  var failedIdx = -1;
  if (failedStage) {
    for (var j = 0; j < STAGES.length; j++) if (STAGES[j].key === failedStage) failedIdx = j;
  }
  var cleanupDone = logText.indexOf('[cleanup]') !== -1;
  var wrap = document.getElementById('pipeline');
  clear(wrap);
  STAGES.forEach(function (s, i) {
    var stage = el('span', 'pipe-stage ' + stageState(i, reached, status, failedIdx, cleanupDone));
    var chip = el('span', 'pipe-chip');
    chip.appendChild(el('span', 'dot'));
    chip.appendChild(el('span', null, t('pipe.' + s.key, s.en)));
    stage.appendChild(chip);
    wrap.appendChild(stage);
    if (i < STAGES.length - 1) wrap.appendChild(el('span', 'pipe-arrow'));
  });
}
function renderFilters() {
  var box = document.getElementById('log-filters');
  clear(box);
  FILTERS.forEach(function (f) {
    var b = el('button', 'btn' + (activeFilter === f.key ? ' active' : ''), t('filter.' + f.key, f.en));
    b.type = 'button';
    b.addEventListener('click', function () { activeFilter = f.key; renderFilters(); renderLogs(); });
    box.appendChild(b);
  });
}
function renderLogs() {
  var pre = document.getElementById('live-log');
  pre.removeAttribute('data-i18n');
  var f = FILTERS.filter(function (x) { return x.key === activeFilter; })[0];
  var lines = lastLogText.split('\n');
  if (f && f.prefixes) {
    lines = lines.filter(function (l) {
      return f.prefixes.some(function (p) { return l.indexOf(p) !== -1; });
    });
  }
  var atBottom = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 40;
  pre.textContent = lines.join('\n') || t('logs.none', '(no logs yet)');
  if (atBottom) pre.scrollTop = pre.scrollHeight;
}
async function loadLogs() {
  try {
    var r = await api('/api/builds/logs?job_id=' + encodeURIComponent(jobID));
    lastLogText = r.logs || '';
    renderLogs();
    var d = await api('/api/builds/detail?job_id=' + encodeURIComponent(jobID));
    renderPipeline(lastLogText, d.status, d.failed_stage);
  } catch (e) { /* next tick */ }
}
renderFilters();
document.getElementById('delete-job').addEventListener('click', async function () {
  if (!confirm(t('detail.delete.confirm', 'Delete this job record?'))) return;
  try {
    await api('/api/builds/delete?job_id=' + encodeURIComponent(jobID), { method: 'DELETE' });
    location.href = '/builds';
  } catch (e) { alert(t('detail.delete.fail', 'Delete failed: ') + e.message); }
});
document.getElementById('refresh').addEventListener('click', function () { load(); loadLogs(); });
load();
loadLogs();
setInterval(load, 10000);
setInterval(loadLogs, 5000);
`

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

const logsContent = `
<div class="page-head" data-job-id="{{.JobID}}" id="head">
  <div><h1 data-i18n="logs.h1">Build Logs</h1><p class="sub mono" id="jid"></p></div>
  <div class="actions">
    <a class="btn" id="back-link" href="#" data-i18n="logs.back">Back to Details</a>
    <button class="btn" id="refresh" data-i18n="common.refresh">Refresh</button>
  </div>
</div>
<div class="card"><div class="card-pad">
  <pre class="log-view" id="log" data-i18n="logs.loading">Loading…</pre>
</div></div>`

const logsJS = `
var jobID = document.getElementById('head').getAttribute('data-job-id');
document.getElementById('jid').textContent = jobID;
document.getElementById('back-link').href = '/build/' + encodeURIComponent(jobID);
async function load() {
  var pre = document.getElementById('log');
  pre.removeAttribute('data-i18n');
  try {
    var r = await api('/api/builds/logs?job_id=' + encodeURIComponent(jobID));
    pre.textContent = r.logs || t('logs.none', '(no logs yet)');
  } catch (e) { pre.textContent = t('logs.fail', 'Failed to load logs: ') + e.message; }
}
document.getElementById('refresh').addEventListener('click', load);
load();
setInterval(load, 5000);
`

// ---------------------------------------------------------------------------
// Monitor (builders + instances)
// ---------------------------------------------------------------------------

const monitorContent = `
<div class="page-head">
  <div><h1 data-i18n="mon.h1">Build Nodes</h1><p class="sub" data-i18n="mon.sub">Static builders and cloud instances</p></div>
  <div class="actions"><button class="btn" id="refresh" data-i18n="common.refresh">Refresh</button></div>
</div>
<h2 class="section-title" data-i18n="mon.builders">Builders</h2>
<div class="builder-grid" id="builders"></div>
<div id="builders-empty"></div>
<h2 class="section-title" data-i18n="mon.instances">Cloud Instances</h2>
<div class="card">
  <div class="table-scroll"><table class="list" aria-label="Cloud instances">
    <thead><tr>
      <th data-i18n="th.instance">Instance</th><th data-i18n="th.provider">Provider</th>
      <th data-i18n="th.status">Status</th><th data-i18n="th.ip">IP</th>
      <th data-i18n="th.created">Created</th><th></th>
    </tr></thead>
    <tbody id="instances"></tbody>
  </table></div>
  <div id="instances-empty"></div>
</div>`

const monitorJS = `
async function load() {
  try {
    var data = await api('/api/builders/status');
    var builders = (data && data.builders) || [];
    var grid = document.getElementById('builders');
    var emptyBox = document.getElementById('builders-empty');
    clear(grid); clear(emptyBox);
    if (!builders.length) {
      emptyBox.appendChild(el('div', 'empty', t('mon.noBuilders', 'No registered builders. Static builders register automatically once SERVER_URL is set; ephemeral cloud instances are not listed here.')));
    }
    builders.forEach(function (b) {
      var c = el('article', 'builder-card');
      c.appendChild(el('h3', null, b.id || '-'));
      c.appendChild(el('p', 'ep mono', b.endpoint || ''));
      c.appendChild(statusBadge(b.status));
      var meta = el('div', 'meta');
      meta.appendChild(el('span', null, t('mon.archLabel', 'arch ') + (b.architecture || '-')));
      meta.appendChild(el('span', null, t('mon.loadLabel', 'load ') + (b.current_load || 0) + '/' + (b.capacity || 0)));
      c.appendChild(meta);
      grid.appendChild(c);
    });
  } catch (e) { showError('builders-empty', e); }
  try {
    var r = await api('/api/instances');
    var list = Array.isArray(r) ? r : (r.instances || []);
    var tb = document.getElementById('instances');
    var emptyBox = document.getElementById('instances-empty');
    clear(tb); clear(emptyBox);
    if (!list.length) { emptyBox.appendChild(el('div', 'empty', t('mon.noInstances', 'No cloud instances running.'))); return; }
    list.forEach(function (i) {
      var tr = el('tr');
      tr.appendChild(el('td', 'mono', i.id || '-'));
      tr.appendChild(el('td', 'sec', i.provider || '-'));
      var st = el('td'); st.appendChild(statusBadge(i.status)); tr.appendChild(st);
      tr.appendChild(el('td', 'mono sec', i.ip_address || i.public_ip || '-'));
      tr.appendChild(el('td', 'sec', fmtTime(i.created_at)));
      var act = el('td');
      var sh = el('a', 'btn', t('mon.shell', 'Shell'));
      sh.href = '/shell/' + encodeURIComponent(i.id);
      act.appendChild(sh);
      tr.appendChild(act);
      tb.appendChild(tr);
    });
  } catch (e) { showError('instances-empty', e); }
}
function onLangChange() { load(); }
document.getElementById('refresh').addEventListener('click', load);
load();
setInterval(load, 15000);
`

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

const settingsContent = `
<div class="page-head">
  <div><h1 data-i18n="set.h1">Settings</h1><p class="sub" data-i18n="set.sub">Cloud build configuration — saved changes apply immediately and override server.conf</p></div>
</div>
<div class="settings-layout">
<nav class="subnav" id="subnav" aria-label="Settings sections">
  <span class="subnav-label" data-i18n="set.cat.general">General</span>
  <a data-sec="general" class="active" data-i18n="set.sec.general">Backend &amp; Test</a>
  <span class="subnav-label" data-i18n="set.cat.infra">Infrastructure</span>
  <a data-sec="pve">Proxmox VE</a>
  <a data-sec="gcp">Google Cloud</a>
  <a data-sec="aws">AWS</a>
  <a data-sec="builders" data-i18n="set.sec.builders">Static Builders</a>
  <a data-sec="mirrors" data-i18n="set.sec.mirrors">Mirrors</a>
  <a data-sec="buildconf" data-i18n="set.sec.buildconf">Build Config</a>
  <span class="subnav-label" data-i18n="set.cat.access">Access</span>
  <a data-sec="ssh" data-i18n="set.sec.ssh">SSH Keys</a>
  <a data-sec="gpg" data-i18n="set.sec.gpg">GPG Signing</a>
  <a data-sec="net" data-i18n="set.sec.net">Network &amp; Delivery</a>
</nav>
<div class="settings-panels">
<form id="settings-form">

<section class="panel active" data-panel="general">
  <div class="card"><h3 class="card-title" data-i18n="set.backend">Build Backend</h3><div class="card-pad form-grid">
    <div class="field">
      <label for="provider" data-i18n="set.provider">Default provider</label>
      <select id="provider">
        <option value="pve">Proxmox VE</option>
        <option value="gcp">Google Cloud</option>
        <option value="aws">AWS (beta)</option>
      </select>
      <p class="hint" data-i18n="set.provider.hint">Used when no static builders are configured; more backends can be added over time</p>
    </div>
    <div class="field">
      <label for="ttl" data-i18n="set.ttl">Idle instance TTL (minutes)</label>
      <input type="number" id="ttl" min="0">
      <p class="hint" data-i18n="set.ttl.hint">Warm idle instances are reused by new builds and auto-destroyed after this idle window; 0 disables</p>
    </div>
  </div>
  <div class="card-pad" style="padding-top:0">
    <div class="field">
      <label for="docker_image" data-i18n="set.dockerimage">Build container image</label>
      <input type="text" id="docker_image" placeholder="gentoo/stage3:latest">
      <p class="hint" data-i18n="set.dockerimage.hint">Pulled on each fresh instance; point it at an image in your local registry (e.g. hub.infra.plz.ac/gentoo/stage3:latest) when the Docker Hub mirror is slow</p>
    </div>
    <div class="field check">
      <input type="checkbox" id="verify_install" checked>
      <label for="verify_install" data-i18n="set.verify">Verify each binpkg installs from the binhost before marking the build successful (recommended)</label>
    </div>
  </div></div>

  <div class="card"><h3 class="card-title" data-i18n="set.testbuild">Test Build</h3><div class="card-pad">
    <p class="hint" style="margin-bottom:10px" data-i18n="set.testbuild.hint">Runs the full pipeline with current settings: provision a builder, emerge the package, collect the artifact into the binhost.</p>
    <div class="form-grid">
      <div class="field">
        <label for="test_pkg" data-i18n="set.testbuild.pkg">Package atom</label>
        <input type="text" id="test_pkg" value="app-misc/jq">
      </div>
    </div>
    <div class="form-actions">
      <button class="btn blue" type="button" id="test-build" data-i18n="set.testbuild.go">Start Test Build</button>
      <span class="save-msg" id="test-build-msg" role="status"></span>
    </div>
    <div id="test-build-result" style="margin-top:10px"></div>
  </div></div>
</section>

<section class="panel" data-panel="pve">
  <div class="card"><h3 class="card-title" data-i18n="set.conn">Connection</h3><div class="card-pad">
    <div class="form-grid">
      <div class="field">
        <label for="pve_endpoint" data-i18n="set.endpoint">API endpoint</label>
        <input type="text" id="pve_endpoint" placeholder="https://pve.example.com:8006">
      </div>
      <div class="field">
        <label for="pve_token_id" data-i18n="set.tokenid">API token ID</label>
        <input type="text" id="pve_token_id" placeholder="root@pam!terraform">
      </div>
      <div class="field">
        <label for="pve_token_secret" data-i18n="set.secret">API token secret</label>
        <input type="password" id="pve_token_secret" autocomplete="off">
        <p class="hint" id="secret-hint"></p>
      </div>
      <div class="field">
        <label for="pve_username" data-i18n="set.pveuser">Username (alternative to token)</label>
        <input type="text" id="pve_username" placeholder="terraform-prov@pve" autocomplete="off">
        <p class="hint" data-i18n="set.pveuser.hint">user@realm format; a token is preferred when both are set</p>
      </div>
      <div class="field">
        <label for="pve_password" data-i18n="set.pvepass">Password</label>
        <input type="password" id="pve_password" autocomplete="off">
        <p class="hint" id="pve-pass-hint"></p>
      </div>
    </div>
    <div class="field check">
      <input type="checkbox" id="pve_insecure">
      <label for="pve_insecure" data-i18n="set.tls">Skip TLS verification (self-signed cert; adding the PVE CA to the system trust store is safer)</label>
    </div>
  </div>
  <div class="card-actions">
    <button class="btn" type="button" id="test" data-i18n="set.test">Test Connection</button>
    <span class="save-msg" id="test-msg" role="status"></span>
  </div></div>

  <div class="card" id="test-card" style="display:none">
    <h3 class="card-title" data-i18n="set.clusternodes">Cluster Nodes</h3>
    <div class="table-scroll"><table class="list" aria-label="Cluster nodes">
      <thead><tr>
        <th data-i18n="th.node">Node</th><th data-i18n="th.status">Status</th>
        <th data-i18n="th.freemem">Free Memory</th><th data-i18n="th.cpu">CPU Load</th>
        <th data-i18n="th.hastpl">Has Template</th>
      </tr></thead>
      <tbody id="test-rows"></tbody>
    </table></div>
  </div>

  <div class="card"><h3 class="card-title" data-i18n="set.placement">Node Scheduling</h3><div class="card-pad">
    <div class="radio-row">
      <label><input type="radio" name="placement" id="place_auto" checked>
        <span data-i18n="set.place.auto">Automatic (recommended)</span></label>
      <label><input type="radio" name="placement" id="place_manual">
        <span data-i18n="set.place.manual">Pin to a node</span></label>
    </div>
    <p class="hint" style="margin-bottom:10px" data-i18n="set.place.auto.hint">Each build queries live cluster load and lands on the least-loaded eligible node</p>
    <div class="form-grid">
      <div class="field" id="manual-node-field" style="display:none">
        <label for="pve_node_manual" data-i18n="set.node">Target node</label>
        <input type="text" id="pve_node_manual" placeholder="pve">
      </div>
      <div class="field" id="candidate-field">
        <label for="pve_nodes" data-i18n="set.nodes">Candidate nodes (optional, comma-separated)</label>
        <input type="text" id="pve_nodes" placeholder="pve1,pve2,pve3">
        <p class="hint" data-i18n="set.nodes.hint">Use on shared-storage clusters; empty restricts to template-hosting nodes</p>
      </div>
    </div>
  </div></div>

  <div class="card"><h3 class="card-title" data-i18n="set.resources">Resources</h3><div class="card-pad form-grid">
    <div class="field">
      <label for="pve_template" data-i18n="set.template">VM template</label>
      <input type="text" id="pve_template" placeholder="debian-12-cloudinit-template">
      <p class="hint" data-i18n="set.template.hint">Must be a QEMU template with cloud-init and qemu-guest-agent installed</p>
    </div>
    <div class="field">
      <label for="pve_storage" data-i18n="set.storage">Storage pool</label>
      <input type="text" id="pve_storage" placeholder="local-lvm">
    </div>
    <div class="field">
      <label for="pve_network" data-i18n="set.bridge">Network bridge</label>
      <input type="text" id="pve_network" placeholder="vmbr0">
    </div>
    <div class="field">
      <label for="pve_nameserver" data-i18n="set.nameserver">DNS server for build VMs (optional)</label>
      <input type="text" id="pve_nameserver" placeholder="10.0.0.252">
      <p class="hint" data-i18n="set.nameserver.hint">Pushed via cloud-init; set your internal DNS so mirror/registry domains resolve on build VMs</p>
    </div>
    <div class="field">
      <label for="pve_cicustom" data-i18n="set.cicustom">cloud-init snippet (cicustom, optional)</label>
      <input type="text" id="pve_cicustom" placeholder="vendor=local:snippets/vendor.yaml">
      <p class="hint" data-i18n="set.cicustom.hint">Preserved on cloned VMs; use a vendor snippet that installs qemu-guest-agent when the base image lacks it</p>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="gcp">
  <div class="card"><h3 class="card-title">Google Cloud</h3><div class="card-pad form-grid">
    <div class="field">
      <label for="gcp_project" data-i18n="set.gcp.project">Project</label>
      <input type="text" id="gcp_project" placeholder="my-project">
    </div>
    <div class="field">
      <label for="gcp_region" data-i18n="set.gcp.region">Region</label>
      <input type="text" id="gcp_region" placeholder="us-central1">
    </div>
    <div class="field">
      <label for="gcp_zone" data-i18n="set.gcp.zone">Zone</label>
      <input type="text" id="gcp_zone" placeholder="us-central1-a">
    </div>
    <div class="field">
      <label for="gcp_key_file" data-i18n="set.gcp.keyfile">Service account key file (path on server)</label>
      <input type="text" id="gcp_key_file" placeholder="/var/lib/portage-engine/gcp-key.json">
    </div>
  </div></div>
</section>

<section class="panel" data-panel="aws">
  <div class="card"><h3 class="card-title">AWS</h3><div class="card-pad form-grid">
    <div class="field">
      <label for="aws_region" data-i18n="set.gcp.region">Region</label>
      <input type="text" id="aws_region" placeholder="us-east-1">
    </div>
    <div class="field">
      <label for="aws_zone" data-i18n="set.gcp.zone">Zone</label>
      <input type="text" id="aws_zone" placeholder="us-east-1a">
    </div>
    <div class="field">
      <label for="aws_access_key" data-i18n="set.aws.ak">Access key ID</label>
      <input type="text" id="aws_access_key" autocomplete="off">
    </div>
    <div class="field">
      <label for="aws_secret_key" data-i18n="set.aws.sk">Secret access key</label>
      <input type="password" id="aws_secret_key" autocomplete="off">
      <p class="hint" id="aws-secret-hint"></p>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="builders">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.builders">Static Builders</h3><div class="card-pad">
    <div class="field">
      <label for="remote_builders" data-i18n="set.builders">Builder URLs (comma-separated)</label>
      <input type="text" id="remote_builders" placeholder="http://builder1:9090,http://builder2:9090">
      <p class="hint" data-i18n="set.builders.hint">When set, builds are dispatched round-robin to these builders; when empty, an ephemeral cloud VM is provisioned per build</p>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="mirrors">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.mirrors">Mirrors</h3><div class="card-pad">
    <p class="hint" style="margin-bottom:10px" data-i18n="set.mirrors.hint">Internal mirrors used when bootstrapping build instances — dramatically faster deploys on a LAN. All optional.</p>
    <div class="form-grid">
      <div class="field">
        <label for="apt_mirror" data-i18n="set.mirrors.apt">APT mirror (base URL)</label>
        <input type="text" id="apt_mirror" placeholder="http://10.31.0.2">
        <p class="hint" data-i18n="set.mirrors.apt.hint">Serves /debian and /debian-security; the guest's sources.list is rewritten to it</p>
      </div>
      <div class="field">
        <label for="docker_dl_mirror" data-i18n="set.mirrors.dockerdl">Docker download mirror</label>
        <input type="text" id="docker_dl_mirror" placeholder="http://10.31.0.2/docker-ce">
        <p class="hint" data-i18n="set.mirrors.dockerdl.hint">Mirror of download.docker.com; fed to the vendored install script via DOWNLOAD_URL</p>
      </div>
      <div class="field">
        <label for="docker_reg_mirror" data-i18n="set.mirrors.dockerreg">Docker registry mirror</label>
        <input type="text" id="docker_reg_mirror" placeholder="https://hub.infra.plz.ac">
        <p class="hint" data-i18n="set.mirrors.dockerreg.hint">Docker Hub mirror written to daemon.json; accelerates the gentoo/stage3 pull</p>
      </div>
      <div class="field">
        <label for="gentoo_mirror" data-i18n="set.mirrors.gentoo">Gentoo mirror (GENTOO_MIRRORS)</label>
        <input type="text" id="gentoo_mirror" placeholder="http://10.31.0.2/gentoo">
        <p class="hint" data-i18n="set.mirrors.gentoo.hint">Distfiles and webrsync snapshots; lands in make.conf on build instances</p>
      </div>
      <div class="field">
        <label for="portage_sync_method" data-i18n="set.mirrors.method">Portage tree sync method</label>
        <select id="portage_sync_method">
          <option value="webrsync">webrsync (snapshot, recommended)</option>
          <option value="rsync">rsync (incremental, needs sync URI)</option>
        </select>
        <p class="hint" data-i18n="set.mirrors.method.hint">webrsync downloads one snapshot tarball from the Gentoo mirror; rsync syncs file-by-file and is slow without a LAN rsync mirror</p>
      </div>
      <div class="field">
        <label for="portage_sync_uri" data-i18n="set.mirrors.sync">Portage sync URI (optional)</label>
        <input type="text" id="portage_sync_uri" placeholder="rsync://mirror/gentoo-portage">
        <p class="hint" data-i18n="set.mirrors.sync.hint">Custom repos.conf sync-uri; empty uses webrsync snapshots from the Gentoo mirror</p>
      </div>
    </div>
  </div></div>
  <div class="card"><h3 class="card-title" data-i18n="set.sec.upload">Artifact Upload</h3><div class="card-pad">
    <p class="hint" data-i18n="set.upload.desc">When set, fresh binpkgs (with the Packages index and signing pubkey) are pushed to the internal mirror's artifact API, and install verification runs against the mirror URL.</p>
    <div class="grid-2">
      <div class="field">
        <label for="upload_url" data-i18n="set.upload.url">Mirror base URL</label>
        <input type="text" id="upload_url" placeholder="http://10.31.0.2">
        <p class="hint" data-i18n="set.upload.url.hint">Empty disables uploading; packages then serve only from this server's /binpkgs</p>
      </div>
      <div class="field">
        <label for="upload_dir" data-i18n="set.upload.dir">Artifact directory</label>
        <input type="text" id="upload_dir" placeholder="portage-engine">
        <p class="hint" data-i18n="set.upload.dir.hint">Files land under /local/&lt;dir&gt;/… — that URL becomes the LAN binhost</p>
      </div>
      <div class="field">
        <label for="upload_user" data-i18n="set.upload.user">Username</label>
        <input type="text" id="upload_user" autocomplete="off">
      </div>
      <div class="field">
        <label for="upload_password" data-i18n="set.upload.pass">Password</label>
        <input type="password" id="upload_password" autocomplete="new-password">
        <p class="hint" id="upload-pass-hint"></p>
      </div>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="buildconf">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.buildconf">Build Config</h3><div class="card-pad">
    <div class="field">
      <label for="make_conf_extra" data-i18n="set.makeconf">Extra make.conf content</label>
      <textarea id="make_conf_extra" spellcheck="false" placeholder="USE=&quot;-doc -test&quot;&#10;ACCEPT_LICENSE=&quot;*&quot;&#10;FEATURES=&quot;parallel-fetch&quot;"></textarea>
      <p class="hint" data-i18n="set.makeconf.hint">Appended verbatim to the generated make.conf on every build instance (global USE, ACCEPT_LICENSE, FEATURES, EMERGE_DEFAULT_OPTS, ...). Per-package USE comes from the client's config bundle.</p>
    </div>
    <div class="field">
      <label for="build_features" data-i18n="set.buildfeatures">Build container FEATURES</label>
      <input type="text" id="build_features" spellcheck="false" placeholder="-userpriv -usersandbox">
      <p class="hint" data-i18n="set.buildfeatures.hint">Appended to the build container's FEATURES. Docker builds need "-userpriv -usersandbox" (no unshare/privilege-drop, so signature verification runs as root). Leave empty only when building in a full Gentoo VM.</p>
    </div>
    <div class="field">
      <label for="build_mode" data-i18n="set.buildmode">Build mode</label>
      <select id="build_mode">
        <option value="docker">Docker container (Debian host + gentoo/stage3)</option>
        <option value="native-gentoo">Native Gentoo VM (UEFI template)</option>
      </select>
      <p class="hint" data-i18n="set.buildmode.hint">Docker: build inside a gentoo/stage3 container on a Debian VM. Native Gentoo VM: provision a Gentoo VM from the UEFI cloud-init template and emerge natively — required for working in-emerge signing. Set the PVE template to your Gentoo template for native mode.</p>
    </div>
    <div class="field field-check">
      <label class="check"><input type="checkbox" id="sign_binpkgs"> <span data-i18n="set.signbinpkgs">Sign binary packages (in-emerge)</span></label>
      <p class="hint" data-i18n="set.signbinpkgs.hint">Enables gpkg signing during emerge. Portage's post-sign self-verification needs getuto's CA to certify the key, which is not achievable inside the Docker build container — enable this only on native Gentoo VM builders. When off, packages build unsigned; the pubkey is still published and install verification still imports it.</p>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="ssh">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.ssh">SSH Keys</h3><div class="card-pad">
    <div class="form-grid">
      <div class="field">
        <label for="ssh_key_path" data-i18n="set.keypath">Private key path</label>
        <input type="text" id="ssh_key_path" placeholder="/var/lib/portage-engine/id_ed25519">
        <p class="hint" data-i18n="set.keypath.hint">The matching .pub is injected into new VMs via cloud-init</p>
      </div>
      <div class="field">
        <label for="ssh_user" data-i18n="set.sshuser">SSH user</label>
        <input type="text" id="ssh_user" placeholder="root">
        <p class="hint" data-i18n="set.sshuser.hint">The deployment script requires root</p>
      </div>
      <div class="field">
        <label for="ssh_known_hosts" data-i18n="set.knownhosts">known_hosts path (optional)</label>
        <input type="text" id="ssh_known_hosts">
      </div>
    </div>
    <div class="field check">
      <input type="checkbox" id="ssh_insecure">
      <label for="ssh_insecure" data-i18n="set.hostkey">Skip SSH host key verification (needed for first connect to fresh VMs, or use known_hosts above)</label>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="gpg">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.gpg">GPG Signing</h3><div class="card-pad">
    <div class="stat-grid" style="margin-bottom:16px">
      <div class="stat-tile">
        <h4 data-i18n="set.gpg.state">Signing</h4>
        <div class="num" id="gpg-state" style="font: var(--title-3-emphasized)">…</div>
      </div>
      <div class="stat-tile">
        <h4 data-i18n="set.gpg.keyid">Key ID</h4>
        <div class="num wrap" id="gpg-keyid">-</div>
      </div>
    </div>
    <div id="gpg-gen-box">
      <div class="form-grid">
        <div class="field">
          <label for="gpg_name" data-i18n="set.gpg.name">Key name</label>
          <input type="text" id="gpg_name" placeholder="Portage Engine">
        </div>
        <div class="field">
          <label for="gpg_email" data-i18n="set.gpg.email">Key email</label>
          <input type="text" id="gpg_email" placeholder="portage@example.com">
        </div>
      </div>
      <div class="form-actions">
        <button class="btn blue" type="button" id="gpg-generate" data-i18n="set.gpg.generate">Generate Key &amp; Enable Signing</button>
        <span class="save-msg" id="gpg-msg" role="status"></span>
      </div>
      <p class="hint" style="margin-top:8px" data-i18n="set.gpg.hint">Creates a signing key on the server (or adopts the configured one) and enables signing. Clients fetch the public key from /api/v1/gpg/public-key; portage-client configure sets verify-signature automatically.</p>
    </div>
  </div></div>
  <div class="card"><h3 class="card-title" data-i18n="set.gpg.pubkey">Public Key</h3><div class="card-pad">
    <pre class="log-view" id="gpg-pubkey" style="max-height:260px">-</pre>
    <div class="form-actions">
      <a class="btn" href="/api/keys/download" data-i18n="set.gpg.download">Download Public Key</a>
    </div>
  </div></div>
</section>

<section class="panel" data-panel="net">
  <div class="card"><h3 class="card-title" data-i18n="set.sec.net">Network &amp; Delivery</h3><div class="card-pad form-grid">
    <div class="field">
      <label for="callback" data-i18n="set.callback">Callback URL</label>
      <input type="text" id="callback" placeholder="http://10.0.0.10:8080">
      <p class="hint" data-i18n="set.callback.hint">How build VMs reach this server; must be an address reachable from the VM</p>
    </div>
    <div class="field">
      <label for="bin_path" data-i18n="set.binpath">Builder binary path</label>
      <input type="text" id="bin_path" placeholder="bin/portage-builder-linux-amd64">
      <p class="hint" data-i18n="set.binpath.hint">Copied to instances via scp; must be linux and arch-matching</p>
    </div>
    <div class="field">
      <label for="bin_url" data-i18n="set.binurl">Builder binary URL (optional)</label>
      <input type="text" id="bin_url" placeholder="https://example.com/portage-builder-linux-amd64">
      <p class="hint" data-i18n="set.binurl.hint">Downloaded by the instance at bootstrap; path wins if both are set</p>
    </div>
  </div></div>
</section>

<div class="settings-footer">
  <button class="btn blue" type="submit" id="save" data-i18n="set.save">Save</button>
  <span class="save-msg" id="msg" role="status"></span>
</div>
</form>
</div>
</div>`

const settingsJS = `
var form = document.getElementById('settings-form');
var msg = document.getElementById('msg');
var lastSettings = null;

/* --- sub-navigation --- */
function showSection(sec) {
  var links = document.querySelectorAll('#subnav a[data-sec]');
  var panels = document.querySelectorAll('.panel');
  for (var i = 0; i < links.length; i++) links[i].classList.toggle('active', links[i].getAttribute('data-sec') === sec);
  for (var j = 0; j < panels.length; j++) panels[j].classList.toggle('active', panels[j].getAttribute('data-panel') === sec);
}
document.getElementById('subnav').addEventListener('click', function (e) {
  var a = e.target.closest('a[data-sec]');
  if (!a) return;
  location.hash = a.getAttribute('data-sec');
  showSection(a.getAttribute('data-sec'));
});
if (location.hash && document.querySelector('.panel[data-panel="' + location.hash.slice(1) + '"]')) {
  showSection(location.hash.slice(1));
}

/* --- placement radio --- */
function syncPlacement() {
  var manual = document.getElementById('place_manual').checked;
  document.getElementById('manual-node-field').style.display = manual ? '' : 'none';
  document.getElementById('candidate-field').style.display = manual ? 'none' : '';
}
document.getElementById('place_auto').addEventListener('change', syncPlacement);
document.getElementById('place_manual').addEventListener('change', syncPlacement);

/* --- form state --- */
function val(id) { return document.getElementById(id).value.trim(); }
function setVal(id, v) { document.getElementById(id).value = (v === undefined || v === null) ? '' : v; }
function checked(id) { return document.getElementById(id).checked; }
function csv(id) {
  return val(id) ? val(id).split(',').map(function (s) { return s.trim(); }).filter(Boolean) : [];
}
function collect() {
  var node = document.getElementById('place_manual').checked ? (val('pve_node_manual') || 'pve') : 'auto';
  return {
    provider: val('provider'),
    instance_ttl_minutes: parseInt(val('ttl') || '0', 10) || 0,
    skip_verify_install: !checked('verify_install'),
    docker_image: val('docker_image'),
    remote_builders: csv('remote_builders'),
    gcp_project: val('gcp_project'),
    gcp_region: val('gcp_region'),
    gcp_zone: val('gcp_zone'),
    gcp_key_file: val('gcp_key_file'),
    aws_region: val('aws_region'),
    aws_zone: val('aws_zone'),
    aws_access_key: val('aws_access_key'),
    aws_secret_key: val('aws_secret_key'),
    pve_endpoint: val('pve_endpoint'),
    pve_node: node,
    pve_nodes: csv('pve_nodes'),
    pve_token_id: val('pve_token_id'),
    pve_token_secret: val('pve_token_secret'),
    pve_username: val('pve_username'),
    pve_password: val('pve_password'),
    pve_insecure: checked('pve_insecure'),
    pve_storage: val('pve_storage'),
    pve_network: val('pve_network'),
    pve_template: val('pve_template'),
    pve_cicustom: val('pve_cicustom'),
    pve_nameserver: val('pve_nameserver'),
    apt_mirror: val('apt_mirror'),
    docker_download_mirror: val('docker_dl_mirror'),
    docker_registry_mirror: val('docker_reg_mirror'),
    gentoo_mirror: val('gentoo_mirror'),
    portage_sync_uri: val('portage_sync_uri'),
    portage_sync_method: val('portage_sync_method') || 'webrsync',
    make_conf_extra: document.getElementById('make_conf_extra').value,
    build_features: val('build_features'),
    build_mode: val('build_mode') || 'docker',
    sign_binpkgs: checked('sign_binpkgs'),
    ssh_key_path: val('ssh_key_path'),
    ssh_user: val('ssh_user'),
    ssh_known_hosts: val('ssh_known_hosts'),
    ssh_insecure_host_key: checked('ssh_insecure'),
    upload_url: val('upload_url'),
    upload_user: val('upload_user'),
    upload_password: val('upload_password'),
    upload_dir: val('upload_dir'),
    server_callback_url: val('callback'),
    builder_binary_path: val('bin_path'),
    builder_binary_url: val('bin_url')
  };
}
function fill(s) {
  lastSettings = s;
  setVal('provider', s.provider || 'pve');
  setVal('ttl', s.instance_ttl_minutes || 0);
  document.getElementById('verify_install').checked = !s.skip_verify_install;
  setVal('docker_image', s.docker_image);
  setVal('remote_builders', (s.remote_builders || []).join(','));
  setVal('gcp_project', s.gcp_project);
  setVal('gcp_region', s.gcp_region);
  setVal('gcp_zone', s.gcp_zone);
  setVal('gcp_key_file', s.gcp_key_file);
  setVal('aws_region', s.aws_region);
  setVal('aws_zone', s.aws_zone);
  setVal('aws_access_key', s.aws_access_key);
  var awsHint = document.getElementById('aws-secret-hint');
  awsHint.textContent = s.has_aws_secret_key ? t('set.secret.saved', 'Saved; leave empty to keep') : t('set.secret.unset', 'Not set yet');
  document.getElementById('aws_secret_key').placeholder = s.has_aws_secret_key ? t('set.secret.ph', 'Saved — leave empty to keep') : '';
  setVal('pve_endpoint', s.pve_endpoint);
  var auto = !s.pve_node || s.pve_node.toLowerCase() === 'auto';
  document.getElementById('place_auto').checked = auto;
  document.getElementById('place_manual').checked = !auto;
  setVal('pve_node_manual', auto ? '' : s.pve_node);
  syncPlacement();
  setVal('pve_nodes', (s.pve_nodes || []).join(','));
  setVal('pve_token_id', s.pve_token_id);
  setVal('pve_username', s.pve_username);
  var passHint = document.getElementById('pve-pass-hint');
  passHint.textContent = s.has_pve_password ? t('set.secret.saved', 'Saved; leave empty to keep') : t('set.secret.unset', 'Not set yet');
  document.getElementById('pve_password').placeholder = s.has_pve_password ? t('set.secret.ph', 'Saved — leave empty to keep') : '';
  document.getElementById('pve_insecure').checked = !!s.pve_insecure;
  setVal('pve_storage', s.pve_storage);
  setVal('pve_network', s.pve_network);
  setVal('pve_template', s.pve_template);
  setVal('pve_cicustom', s.pve_cicustom);
  setVal('pve_nameserver', s.pve_nameserver);
  setVal('apt_mirror', s.apt_mirror);
  setVal('docker_dl_mirror', s.docker_download_mirror);
  setVal('docker_reg_mirror', s.docker_registry_mirror);
  setVal('gentoo_mirror', s.gentoo_mirror);
  setVal('portage_sync_uri', s.portage_sync_uri);
  setVal('portage_sync_method', s.portage_sync_method || 'webrsync');
  setVal('make_conf_extra', s.make_conf_extra);
  setVal('build_features', s.build_features);
  setVal('build_mode', s.build_mode || 'docker');
  document.getElementById('sign_binpkgs').checked = !!s.sign_binpkgs;
  setVal('ssh_key_path', s.ssh_key_path);
  setVal('ssh_user', s.ssh_user);
  setVal('ssh_known_hosts', s.ssh_known_hosts);
  document.getElementById('ssh_insecure').checked = !!s.ssh_insecure_host_key;
  setVal('upload_url', s.upload_url);
  setVal('upload_user', s.upload_user);
  setVal('upload_dir', s.upload_dir);
  var upHint = document.getElementById('upload-pass-hint');
  if (upHint) {
    upHint.textContent = s.has_upload_password ? t('set.secret.saved', 'Saved; leave empty to keep') : t('set.secret.unset', 'Not set yet');
    document.getElementById('upload_password').placeholder = s.has_upload_password ? t('set.secret.ph', 'Saved — leave empty to keep') : '';
  }
  setVal('callback', s.server_callback_url);
  setVal('bin_path', s.builder_binary_path);
  setVal('bin_url', s.builder_binary_url);
  var hint = document.getElementById('secret-hint');
  hint.textContent = s.has_pve_token_secret ? t('set.secret.saved', 'Saved; leave empty to keep') : t('set.secret.unset', 'Not set yet');
  document.getElementById('pve_token_secret').placeholder = s.has_pve_token_secret ? t('set.secret.ph', 'Saved — leave empty to keep') : '';
}
function onLangChange() { if (lastSettings) fill(lastSettings); }
function noteAt(target, text, ok) {
  target.textContent = text;
  target.className = 'save-msg ' + (ok ? 'ok' : 'err');
}
function note(text, ok) { noteAt(msg, text, ok); }

async function saveSettings() {
  var saved = await api('/api/settings/cloud', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(collect())
  });
  fill(saved);
  setVal('pve_token_secret', '');
  setVal('aws_secret_key', '');
  return saved;
}
async function load() {
  try { fill(await api('/api/settings/cloud')); }
  catch (e) { note(t('set.loadfail', 'Failed to load settings: ') + e.message, false); }
}
form.addEventListener('submit', async function (e) {
  e.preventDefault();
  note(t('set.saving', 'Saving…'), true);
  try {
    await saveSettings();
    setVal('pve_password', '');
    note(t('set.saved', 'Saved — in effect immediately'), true);
  } catch (ex) { note(t('set.savefail', 'Save failed: ') + ex.message, false); }
});

/* --- PVE connection test --- */
document.getElementById('test').addEventListener('click', async function () {
  var tmsg = document.getElementById('test-msg');
  noteAt(tmsg, t('set.testing', 'Connecting to PVE…'), true);
  var card = document.getElementById('test-card');
  var tb = document.getElementById('test-rows');
  try {
    var r = await api('/api/settings/cloud/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(collect())
    });
    if (!r.ok) { card.style.display = 'none'; noteAt(tmsg, t('set.testfail', 'Connection failed: ') + (r.error || '-'), false); return; }
    clear(tb);
    (r.nodes || []).forEach(function (n) {
      var tr = el('tr');
      tr.appendChild(el('td', null, n.node));
      var st = el('td'); st.appendChild(statusBadge(n.status)); tr.appendChild(st);
      tr.appendChild(el('td', 'sec', (n.free_mem_gb || 0).toFixed(1) + ' GB'));
      tr.appendChild(el('td', 'sec', ((n.cpu_load || 0) * 100).toFixed(0) + '%'));
      tr.appendChild(el('td', 'sec', n.has_template ? t('set.yes', 'yes') : t('set.no', 'no')));
      tb.appendChild(tr);
    });
    card.style.display = '';
    noteAt(tmsg, t('set.testok', 'Connected — found %d node(s)').replace('%d', (r.nodes || []).length), true);
  } catch (ex) { card.style.display = 'none'; noteAt(tmsg, t('set.testfail', 'Connection failed: ') + ex.message, false); }
});

/* --- full-pipeline test build --- */
var testPoll = null;
document.getElementById('test-build').addEventListener('click', async function () {
  var tmsg = document.getElementById('test-build-msg');
  var box = document.getElementById('test-build-result');
  var pkg = val('test_pkg') || 'app-misc/jq';
  try {
    noteAt(tmsg, t('set.testbuild.saving', 'Saving settings…'), true);
    await saveSettings();
    noteAt(tmsg, t('set.testbuild.submitting', 'Submitting build…'), true);
    var r = await api('/api/builds/submit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ package_name: pkg, arch: 'amd64' })
    });
    var jobID = r.job_id;
    noteAt(tmsg, t('set.testbuild.submitted', 'Submitted — job ') + jobID.slice(0, 8), true);
    clear(box);
    var line = el('div', 'status gray');
    line.appendChild(el('span', 'dot'));
    var stText = el('span', null, '…');
    line.appendChild(stText);
    var link = el('a', null, ' ' + t('set.testbuild.view', 'View build details'));
    link.href = '/build/' + encodeURIComponent(jobID);
    box.appendChild(line);
    box.appendChild(link);
    if (testPoll) clearInterval(testPoll);
    testPoll = setInterval(async function () {
      try {
        var b = await api('/api/builds/detail?job_id=' + encodeURIComponent(jobID));
        clear(line);
        var badge = statusBadge(b.status);
        line.className = '';
        line.appendChild(badge);
        if (b.error) line.appendChild(el('span', 'sec', ' ' + b.error.slice(0, 160)));
        if (b.status === 'failed' || b.status === 'completed' || b.status === 'success') clearInterval(testPoll);
      } catch (e) { /* keep polling */ }
    }, 5000);
  } catch (ex) { noteAt(tmsg, t('set.testbuild.fail', 'Test build failed: ') + ex.message, false); }
});

/* --- GPG signing --- */
async function loadGPG() {
  try {
    var s = await api('/api/gpg/status');
    document.getElementById('gpg-state').textContent = s.enabled ? t('set.gpg.on', 'Enabled') : t('set.gpg.off', 'Disabled');
    document.getElementById('gpg-keyid').textContent = s.key_id || '-';
    if (s.enabled) {
      try {
        var r = await fetch('/api/keys/public');
        if (r.ok) document.getElementById('gpg-pubkey').textContent = await r.text();
      } catch (e) {}
    }
  } catch (e) {
    document.getElementById('gpg-state').textContent = '?';
  }
}
document.getElementById('gpg-generate').addEventListener('click', async function () {
  var gmsg = document.getElementById('gpg-msg');
  noteAt(gmsg, t('set.gpg.working', 'Generating key…'), true);
  try {
    var r = await api('/api/gpg/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: val('gpg_name'), email: val('gpg_email') })
    });
    noteAt(gmsg, t('set.gpg.done', 'Signing enabled, key ') + r.key_id, true);
    loadGPG();
  } catch (ex) { noteAt(gmsg, t('set.gpg.fail', 'Failed: ') + ex.message, false); }
});
loadGPG();

load();
`

// ---------------------------------------------------------------------------
// Docs
// ---------------------------------------------------------------------------

const docsContent = `
<div class="page-head"><div><h1 data-i18n="docs.h1">Docs</h1><p class="sub" data-i18n="docs.sub">Quick start</p></div></div>
<div class="card"><div class="card-pad docs-body">
  <h2 data-i18n="docs.consume">Consume binary packages</h2>
  <p data-i18n="docs.consume.p">Point any Gentoo machine at this server as its binhost. From then on emerge fetches and verifies binary packages automatically, falling back to source builds when a package is missing:</p>
  <pre>sudo portage-client configure -server=http://SERVER:8080
emerge --getbinpkg app-misc/jq</pre>
  <h2 data-i18n="docs.build">Request a build</h2>
  <p data-i18n="docs.build.p">Portage has no native way to request a remote build — use the client (add -wait to block until done):</p>
  <pre>portage-client build -server=http://SERVER:8080 -package app-misc/jq -wait</pre>
  <p data-i18n="docs.build.p2">The server provisions an ephemeral build VM (or dispatches to a static builder); the artifact lands in the binhost repository and the Packages index refreshes automatically.</p>
  <h2 data-i18n="docs.cloud">Cloud build configuration</h2>
  <p data-i18n="docs.cloud.p">PVE / cloud provider settings are managed on the Settings page and apply on save. For PVE template creation and the end-to-end test walkthrough, see docs/PVE_TESTING.md in the repository.</p>
  <h2 data-i18n="docs.gpg">GPG verification</h2>
  <p data-i18n="docs.gpg.p">With signing enabled, the client configures verify-signature automatically; the public key is served from /api/v1/gpg/public-key.</p>
</div></div>`

const docsJS = `/* static page */`

// shellHTML is the full-screen web terminal page.
const shellHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title data-i18n="title.shell">Shell — Portage Engine</title>
<link rel="stylesheet" href="/static/apple.css">
<link rel="stylesheet" href="/static/xterm.css">
<style>
  .shell-head { display: flex; align-items: center; gap: 12px; padding: 10px 16px; border-bottom: .5px solid var(--labelDivider); }
  .shell-head .brand { font: var(--body-emphasized); }
  .shell-head .iid { font: 500 12px var(--font-mono); color: var(--systemSecondary); }
  #term { padding: 12px; height: calc(100vh - 46px); box-sizing: border-box; background: #000; }
  .xterm { height: 100%; }
</style>
</head>
<body>
<div class="shell-head">
  <a class="btn" href="/monitor" data-i18n="shell.back">Back</a>
  <span class="brand" data-i18n="shell.title">Instance Shell</span>
  <span class="iid" id="iid" data-instance-id="{{.InstanceID}}"></span>
  <span class="save-msg" id="shell-status"></span>
</div>
<div id="term"></div>
<script src="/static/xterm.js"></script>
<script>` + i18nJS + `
var instanceID = document.getElementById('iid').getAttribute('data-instance-id');
document.getElementById('iid').textContent = instanceID;
var statusEl = document.getElementById('shell-status');
var term = new Terminal({ fontSize: 13, fontFamily: 'ui-monospace, SF Mono, Menlo, monospace', cursorBlink: true, cols: 220, rows: 50, theme: { background: '#000000' } });
term.open(document.getElementById('term'));
var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
var ws = new WebSocket(proto + location.host + '/api/shell?id=' + encodeURIComponent(instanceID));
ws.binaryType = 'arraybuffer';
ws.onopen = function () { statusEl.textContent = t('shell.connected', 'connected'); statusEl.className = 'save-msg ok'; term.focus(); };
ws.onclose = function () { statusEl.textContent = t('shell.closed', 'disconnected'); statusEl.className = 'save-msg err'; };
ws.onerror = function () { statusEl.textContent = t('shell.error', 'connection error'); statusEl.className = 'save-msg err'; };
ws.onmessage = function (ev) {
  if (typeof ev.data === 'string') term.write(ev.data);
  else term.write(new Uint8Array(ev.data));
};
term.onData(function (data) { if (ws.readyState === 1) ws.send(data); });
</script>
</body>
</html>`

// Assembled authed pages.
var (
	overviewHTML    = appPage("Overview", "title.overview", "overview", overviewContent, overviewJS)
	buildsPageHTML  = appPage("Builds", "title.builds", "builds", buildsContent, buildsJS)
	buildDetailHTML = appPage("Build Details", "title.detail", "builds", buildDetailContent, buildDetailJS)
	logsPageHTML    = appPage("Build Logs", "title.logs", "builds", logsContent, logsJS)
	monitorHTML     = appPage("Build Nodes", "title.monitor", "monitor", monitorContent, monitorJS)
	settingsHTML    = appPage("Settings", "title.settings", "settings", settingsContent, settingsJS)
	docsHTML        = appPage("Docs", "title.docs", "docs", docsContent, docsJS)
)
