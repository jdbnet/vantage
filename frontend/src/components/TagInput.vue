<script setup lang="ts">
import { computed, ref } from "vue";

const props = defineProps<{
  modelValue: string;
  suggestions: string[];
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const focused = ref(false);

function normalizeTag(raw: string): string {
  return raw.trim().toLowerCase().replace(/\s+/g, "");
}

function selectedTags(): Set<string> {
  return new Set(parseTagsInput(props.modelValue));
}

function parseTagsInput(raw: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const part of raw.split(",")) {
    const name = normalizeTag(part);
    if (!name || seen.has(name)) continue;
    seen.add(name);
    tags.push(name);
  }
  return tags;
}

function currentPartial(): string {
  const val = props.modelValue;
  const idx = val.lastIndexOf(",");
  return normalizeTag(idx >= 0 ? val.slice(idx + 1) : val);
}

const filteredSuggestions = computed(() => {
  const partial = currentPartial();
  const selected = selectedTags();
  return props.suggestions
    .filter((tag) => {
      if (selected.has(tag)) return false;
      if (!partial) return true;
      return tag.includes(partial);
    })
    .slice(0, 8);
});

const showSuggestions = computed(
  () => focused.value && filteredSuggestions.value.length > 0,
);

function applySuggestion(tag: string) {
  const val = props.modelValue;
  const idx = val.lastIndexOf(",");
  const prefix = idx >= 0 ? `${val.slice(0, idx + 1)} ` : "";
  emit("update:modelValue", `${prefix}${tag}, `);
}

function onBlur() {
  window.setTimeout(() => {
    focused.value = false;
  }, 150);
}
</script>

<template>
  <div class="relative">
    <input
      :value="modelValue"
      placeholder="buildagents, prod"
      autocomplete="off"
      class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @focus="focused = true"
      @blur="onBlur"
    />
    <ul
      v-if="showSuggestions"
      class="absolute z-20 mt-1 max-h-40 w-full overflow-auto rounded-lg border border-slate-700 bg-surface-raised py-1 shadow-lg"
    >
      <li v-for="tag in filteredSuggestions" :key="tag">
        <button
          type="button"
          class="block w-full px-3 py-1.5 text-left text-sm text-slate-200 hover:bg-slate-800"
          @mousedown.prevent="applySuggestion(tag)"
        >
          {{ tag }}
        </button>
      </li>
    </ul>
  </div>
</template>
