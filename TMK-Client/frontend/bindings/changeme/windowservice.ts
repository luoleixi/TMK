// This file follows the generated Wails binding shape.

import { Call as $Call, CancellablePromise as $CancellablePromise } from "@wailsio/runtime";

export function ShowMain(): $CancellablePromise<void> {
    return $Call.ByName("main.WindowService.ShowMain");
}

export function HideMain(): $CancellablePromise<void> {
    return $Call.ByName("main.WindowService.HideMain");
}

export function ShowSubtitle(): $CancellablePromise<void> {
    return $Call.ByName("main.WindowService.ShowSubtitle");
}

export function HideSubtitle(): $CancellablePromise<void> {
    return $Call.ByName("main.WindowService.HideSubtitle");
}

export function ToggleSubtitle(): $CancellablePromise<boolean> {
    return $Call.ByName("main.WindowService.ToggleSubtitle");
}
