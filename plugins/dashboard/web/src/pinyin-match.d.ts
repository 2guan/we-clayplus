// pinyin-match 固定为 1.2.10；使用源码入口让搜索和排序共享字典。
declare module "pinyin-match/src/simplified.js" {
  const pinyin: { match(input: string, keys: string): [number, number] | false };
  export default pinyin;
}
declare module "pinyin-match/src/simplified_dict.js" {
  export const dict: Record<string, string>;
}
