// This file follows the generated Wails binding shape.

import { Call as $Call, CancellablePromise as $CancellablePromise } from "@wailsio/runtime";

export type UserSettings = {
    "source_lang": string;
    "target_lang": string;
    "selected_device": number;
    "subtitle_mounted": boolean;
    "history_keyword": string;
    "history_date_from": string;
    "history_date_to": string;
};

export function Load(): $CancellablePromise<UserSettings> {
    return $Call.ByName("main.SettingsService.Load");
}

export function Save(settings: UserSettings): $CancellablePromise<void> {
    return $Call.ByName("main.SettingsService.Save", settings);
}
