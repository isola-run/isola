"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.encodeAsFormParameter = encodeAsFormParameter;
const qs_1 = __importDefault(require("qs"));
/**
 * Takes an unknown value, stringifies it using qs, and parses it into a key-value record
 */
function encodeAsFormParameter(value) {
    const stringified = qs_1.default.stringify(value, { encode: false });
    const keyValuePairs = stringified.split("&").map((pair) => {
        const [key, value] = pair.split("=");
        return [key, value];
    });
    return Object.fromEntries(keyValuePairs);
}
