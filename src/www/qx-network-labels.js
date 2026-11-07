// Display labels for the address page's network selectors. The option values
// remain the gateway's original network IDs; no payment configuration changes.
const networkNames = {
  tron: ['波场 TRON（TRC20）', 'TRON (TRC20)'],
  ethereum: ['以太坊 Ethereum（ERC20）', 'Ethereum (ERC20)'],
  binance: ['BNB Smart Chain（BSC / BEP20）', 'BNB Smart Chain (BSC / BEP20)'],
  solana: ['Solana（SPL）', 'Solana (SPL)'],
  polygon: ['Polygon PoS 网络', 'Polygon PoS'],
  plasma: ['Plasma 网络', 'Plasma'],
  ton: ['TON（The Open Network）', 'TON (The Open Network)'],
  aptos: ['Aptos 网络', 'Aptos'],
};

export function networkLabel(network, fallback = network) {
  const locale = document.cookie.match(/(?:^|; )PARAGLIDE_LOCALE=([^;]+)/)?.[1]
    || navigator.language;
  const names = Object.hasOwn(networkNames, network) ? networkNames[network] : null;
  return names ? names[locale.startsWith('zh') ? 0 : 1] : fallback;
}
