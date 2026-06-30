// This file follows the generated Wails binding shape.

import { Call as $Call, CancellablePromise as $CancellablePromise } from "@wailsio/runtime";

export type ExportRecord = {
    "source_text": string;
    "translated_text": string;
    "sequence": number;
};

export function ExportTXT(title: string, records: ExportRecord[]): $CancellablePromise<string> {
    return $Call.ByName("main.ExportService.ExportTXT", title, records);
}

export function ExportSRT(title: string, records: ExportRecord[]): $CancellablePromise<string> {
    return $Call.ByName("main.ExportService.ExportSRT", title, records);
}
