import PinyinMatch from "pinyin-match/src/simplified.js";
import { dict } from "pinyin-match/src/simplified_dict.js";
import type { Contact } from "./types";

const names = new Intl.Collator("zh-CN-u-co-pinyin", { numeric: true, sensitivity: "base" });
const alphabet = new Intl.Collator("en", { numeric: true, sensitivity: "base" });
// 与搜索共用已安装的拼音字典，避免为排序再引入一份依赖或字典。
const readings = new Map<string, string>();
for (const [pinyin, characters] of Object.entries(dict)) {
  for (const character of characters) {
    if (!readings.has(character)) readings.set(character, pinyin);
  }
}
const spelling = (name: string) => Array.from(name, character => readings.get(character) || character).join("");
export function compareContacts(a: Contact, b: Contact) {
  const left = (a.remark || a.name).trim();
  const right = (b.remark || b.name).trim();
  // 中文按拼音排列；数字、标点、表情及空名称归入末尾的特殊名称。
  const special = (name: string) => !/^[a-z\p{Script=Han}]/iu.test(name);
  return Number(special(left)) - Number(special(right)) || alphabet.compare(spelling(left), spelling(right)) || names.compare(left, right) || a.id.localeCompare(b.id);
}

// 昵称和备注支持全拼、首字母；ID、标签及消息预览继续支持普通文字搜索。
export function matchesContact(c: Contact, search: string, preview = "") {
  const query = search.trim().toLowerCase();
  if (!query) return true;
  return [c.name, c.remark].some(value => value &&
    (value.toLowerCase().includes(query) || !!PinyinMatch.match(value, query))) ||
    [c.id, c.alias || "", ...c.tags, preview].some(value => value.toLowerCase().includes(query));
}
