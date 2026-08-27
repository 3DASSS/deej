# Changelog

## v0.3.1 → current

### New Features

- **Profiles** — Keep several slider mappings and switch between them instantly. Each profile has its own `slider_mapping`; everything else (`com`, `obs`, `language`, ...) stays shared. Switch from the settings window's _Profiles_ tab, the profile dropdown in the title bar, the tray _Profiles_ menu, or a per-profile global hotkey that works even when deej isn't focused:
  ```yaml
  active_profile: default
  profiles:
    - name: default
      slider_mapping:
        0: master
        1: discord.exe
    - name: gaming
      slider_mapping:
        0: master
        1: [game.exe, discord.exe]
      hotkey: Ctrl+Alt+2      # activates this profile from anywhere
  ```
  Existing configs with a top-level `slider_mapping` are read as before and migrated into a single `default` profile the next time settings are saved.
- **OBS integration** — Control OBS audio source volumes with your sliders. Enable WebSocket support in OBS via _Tools → WebSocket Server Settings_, then add to your config:
  ```yaml
  slider_mapping:
    0: deej.obs:Mic/Aux
    1: deej.obs:Desktop Audio

  obs:
    enabled: true
    host: localhost
    port: 4455
    password: ""        # set if you use a password in OBS
    volume_conversion: true
  ```
  Keep `volume_conversion` enabled (the default) so the physical slider stays aligned with the fader shown in OBS and offers finer control at low volumes. Set it to `false` if you prefer the volume to rise faster as you move the slider.
- **Discord integration** — Control the volume of individual people in a voice call, plus your own microphone and output levels inside Discord. Mapping the `discord.exe` process only ever moved all of Discord at once; this reaches inside it:
  ```yaml
  slider_mapping:
    0: deej.discord:Nikita     # one person in your voice channel
    1: deej.discord.input      # your own mic level in Discord
    2: deej.discord.output     # Discord's own output level

  discord:
    enabled: true
    client_id: ""
    client_secret: ""
    volume_conversion: true
  ```
  For individual participant targets, keep `volume_conversion` enabled (the default) so the physical slider stays aligned with Discord's on-screen volume control and offers finer control at low volumes. Set it to `false` if you prefer the volume to rise faster. This setting does not affect `deej.discord.input` or `deej.discord.output`.
  Discord only allows this for an application you own, so deej can't ship its own credentials. Create one at [discord.com/developers](https://discord.com/developers/applications), add `http://localhost` as a redirect URI under _OAuth2_, paste its client ID and secret into the settings window's _Discord_ tab, and click _Link Discord account_ — Discord asks you to approve it once, and deej reconnects on its own from then on. The target picker lists whoever is in your voice channel, and a name that isn't there yet can be typed in by hand; it starts working once they join.
- **Named audio device support on Linux** — Sliders can now be mapped to specific audio devices by name on Linux, same as Windows. Input devices are suffixed with `(input)` (e.g. `hyperx cloud (input)`), so a card's input and output can be controlled separately even when they share the same device name.
- **Event-driven session tracking** — deej now listens for system audio events instead of polling, so sliders respond correctly as soon as an app opens or closes.

### Improvements

- Serial connection settings now live in a `com:` config section:
  ```yaml
  com:
    port: auto
    baud_rate: 9600
    vid: 0x1A86
    pid: 0x7523
  ```
  The old flat keys (`com_port`, `baud_rate`, `com_vid`, `com_pid`) are still read, but saving from the settings GUI writes the new format.
- Linux tray menu fixes.
- Updated Go version and dependencies.

---

## v0.3.1 → текущая версия

### Новые возможности

- **Профили** — Храните несколько раскладок слайдеров и мгновенно переключайтесь между ними. У каждого профиля своя `slider_mapping`; всё остальное (`com`, `obs`, `language`, ...) общее. Переключайтесь на вкладке _Профили_ в окне настроек, через выпадающий список в заголовке окна, в меню трея _Профили_ или глобальной горячей клавишей профиля, которая работает, даже когда deej не в фокусе:
  ```yaml
  active_profile: default
  profiles:
    - name: default
      slider_mapping:
        0: master
        1: discord.exe
    - name: gaming
      slider_mapping:
        0: master
        1: [game.exe, discord.exe]
      hotkey: Ctrl+Alt+2      # активирует этот профиль откуда угодно
  ```
  Существующие конфиги с `slider_mapping` на верхнем уровне читаются как раньше и переносятся в один профиль `default` при следующем сохранении настроек.
- **Интеграция с OBS** — Управляйте громкостью аудиоисточников OBS через слайдеры. Включите OBS WebSocket v5 в OBS через _Сервис → Настройки сервера WebSocket_, затем добавьте в конфиг:
  ```yaml
  slider_mapping:
    0: deej.obs:Mic/Aux
    1: deej.obs:Desktop Audio
  
  obs:
    enabled: true
    host: localhost
    port: 4455
    password: ""        # укажите, если в OBS задан пароль
  ```
- **Поддержка именованных устройств на Linux** — Слайдеры теперь можно привязать к конкретным аудиоустройствам по имени на Linux, как и на Windows. Устройства ввода получают суффикс `(input)` (например, `hyperx cloud (input)`), поэтому вход и выход одной карты управляются отдельно, даже если у них одинаковое имя.
- **Отслеживание сессий на основе событий** — deej теперь слушает системные аудиособытия вместо периодического опроса, поэтому слайдеры реагируют корректно сразу после открытия или закрытия приложения.

### Улучшения

- Настройки COM-порта теперь находятся в секции `com:` конфига:
  ```yaml
  com:
    port: auto
    baud_rate: 9600
    vid: 0x1A86
    pid: 0x7523
  ```
  Старые ключи (`com_port`, `baud_rate`, `com_vid`, `com_pid`) по-прежнему читаются, но сохранение из GUI настроек записывает новый формат.
- Исправления для меню в трее на Linux.
- Обновлена версия Go и зависимости.
