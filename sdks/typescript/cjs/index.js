"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.IsolaTimeoutError = exports.IsolaError = exports.IsolaEnvironment = exports.IsolaClient = exports.Isola = void 0;
exports.Isola = __importStar(require("./api/index.js"));
var ClientWrapper_js_1 = require("./ClientWrapper.js");
Object.defineProperty(exports, "IsolaClient", { enumerable: true, get: function () { return ClientWrapper_js_1.IsolaClient; } });
var environments_js_1 = require("./environments.js");
Object.defineProperty(exports, "IsolaEnvironment", { enumerable: true, get: function () { return environments_js_1.IsolaEnvironment; } });
var index_js_1 = require("./errors/index.js");
Object.defineProperty(exports, "IsolaError", { enumerable: true, get: function () { return index_js_1.IsolaError; } });
Object.defineProperty(exports, "IsolaTimeoutError", { enumerable: true, get: function () { return index_js_1.IsolaTimeoutError; } });
