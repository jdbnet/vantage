<script setup lang="ts">
import { ref } from "vue";
import type { SnippetRow } from "@/api";

const props = defineProps<{
  snippet?: SnippetRow | null;
}>();

const emit = defineEmits<{
  (e: "save", data: { label: string; command: string }): void;
  (e: "cancel"): void;
}>();

const label = ref(props.snippet?.label || "");
const command = ref(props.snippet?.command || "");

function submit() {
  if (!label.value.trim() || !command.value.trim()) return;
  emit("save", {
    label: label.value.trim(),
    command: command.value.trim(),
  });
}
</script>

<template>
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
  >
    <div
      class="w-full max-w-md rounded-xl border border-slate-700 bg-surface shadow-2xl"
    >
      <div class="flex items-center justify-between border-b border-slate-800 p-4">
        <h2 class="text-lg font-semibold text-white">
          {{ snippet ? "Edit snippet" : "Add snippet" }}
        </h2>
        <button
          type="button"
          class="text-slate-400 hover:text-white"
          @click="emit('cancel')"
        >
          &times;
        </button>
      </div>
      <form class="space-y-4 p-4" @submit.prevent="submit">
        <div>
          <label class="mb-1 block text-sm font-medium text-slate-300">
            Label
          </label>
          <input
            v-model="label"
            type="text"
            required
            class="w-full rounded-lg border border-slate-700 bg-surface-overlay px-3 py-2 text-sm text-white placeholder:text-slate-500 focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent"
            placeholder="e.g. Docker logs"
            autocomplete="off"
          />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-slate-300">
            Command
          </label>
          <textarea
            v-model="command"
            required
            rows="3"
            class="w-full rounded-lg border border-slate-700 bg-surface-overlay px-3 py-2 text-sm font-mono text-white placeholder:text-slate-500 focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent"
            placeholder="docker compose logs -f"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm font-medium text-slate-300 hover:text-white"
            @click="emit('cancel')"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-black hover:bg-[#16966b]"
          >
            Save snippet
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
