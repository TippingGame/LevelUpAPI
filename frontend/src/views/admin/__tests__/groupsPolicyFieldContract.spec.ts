import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const groupsViewSource = readFileSync(
  resolve(currentDir, "../GroupsView.vue"),
  "utf8",
);

describe("groups policy field contract", () => {
  it("binds batch image and peak-rate fields in both forms", () => {
    for (const form of ["createForm", "editForm"]) {
      for (const field of [
        "allow_batch_image_generation",
        "batch_image_discount_multiplier",
        "batch_image_hold_multiplier",
        "peak_rate_enabled",
        "peak_start",
        "peak_end",
        "peak_rate_multiplier",
      ]) {
        expect(groupsViewSource).toContain(`${form}.${field}`);
      }
    }
  });

  it("preserves extended policy fields across edit hydration and both payloads", () => {
    for (const field of [
      "allow_batch_image_generation",
      "batch_image_discount_multiplier",
      "batch_image_hold_multiplier",
      "peak_rate_enabled",
      "peak_start",
      "peak_end",
      "peak_rate_multiplier",
    ]) {
      expect(groupsViewSource).toContain(`editForm.${field} =`);
    }

    expect(groupsViewSource).toContain(
      "models_list_config: buildModelsListConfig(createModelsListState)",
    );
    expect(groupsViewSource).toContain(
      "models_list_config: buildModelsListConfig(editModelsListState)",
    );
    expect(groupsViewSource).toContain(
      "resetDisabledBatchImagePricing(requestData)",
    );
    expect(groupsViewSource).toContain(
      "resetDisabledBatchImagePricing(payload)",
    );
    expect(groupsViewSource).toContain(
      "requestData.peak_rate_multiplier = normalizeNonNegativeMultiplier",
    );
    expect(groupsViewSource).toContain(
      "payload.peak_rate_multiplier = normalizeNonNegativeMultiplier",
    );
  });
});
