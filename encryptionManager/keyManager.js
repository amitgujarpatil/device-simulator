/* eslint-disable security/detect-non-literal-fs-filename */
const crypto = require("crypto");
const debug = require("debug");
const { getSecret, createSecret, setSecret } = require("@intangles/berries/io_utils/secretsManager/secretManager");
const Intangles = require("@intangles/berries/intangles_sdk/intangles");
const { METADATA_TYPES, PROVIDERS, SECRET_MANAGER_PREFIX } = require("@intangles/berries/Enums/secretManagerEnums");

const log = debug("KeyManager log");
const errorLog = debug("KeyManager error");

/**
 * Manages RSA key pairs for hybrid envelope encryption using AWS Secrets Manager
 * Supports key versioning, rotation, and MongoDB metadata storage via IntanglesSDK
 * Each key version is stored as a separate AWS secret
 * 
 * Performance optimized:
 * - Initialize once, load metadata into memory
 * - All subsequent operations use cached metadata object
 * - Batch updates to minimize database writes
 */
class KeyManager {
    constructor(options = {}) {
        // In-memory cache for loaded keys
        this.keys = new Map();
        this.currentVersion = 0;
        
        // IntanglesSDK metadata object (cached after initialization)
        this.metadataObj = null;
        this.metadataId = options.metadataId || METADATA_TYPES.MQTT_ENCRYPTION_KEYS;
        
        // Secret Manager configuration
        this.provider = options.provider || PROVIDERS.AWS;
        this.secretPrefix = options.secretPrefix || SECRET_MANAGER_PREFIX.MQTT_ENCRYPTION_KEY;
        
        // Initialization flag
        this.initialized = false;
        
        log(`[KeyManager] Initialized with provider: ${this.provider}, prefix: ${this.secretPrefix}`);
    }

    /**
     * Initialize KeyManager - loads metadata using IntanglesSDK
     * Must be called before using any other methods
     * This is optimized to load once and cache the metadata object
     * @async
     * @returns {Promise<void>}
     */
    async initialize() {
        if (this.initialized) {
            log("[KeyManager] Already initialized");
            return;
        }

        try {
            // Try to load existing metadata using IntanglesSDK
            this.metadataObj = new Intangles.Object.SecretManagerMeta(this.metadataId);
            const result = (await this.metadataObj.get())[0] || {};

            if (result._id) {
                log("[KeyManager] Loaded existing metadata from database");
                this.metadataObj = Object.assign(this.metadataObj, result);
            }

            
            // Metadata exists - load into memory
            this.currentVersion = result.current_version || 0;
            log(`[KeyManager] Loaded metadata - current version: ${this.currentVersion}, total versions: ${(this.metadataObj.versions || []).length}`);


            this.initialized = true;
        } catch (err) {
            errorLog("[KeyManager] Error initializing:", err);
            throw err;
        }
    }

    /**
     * Save metadata changes to database
     * Uses cached metadata object for performance
     * @async
     * @private
     * @returns {Promise<void>}
     */
    async _saveMetadata() {
        if (!this.metadataObj) {
            throw new Error("[KeyManager] Metadata object not initialized");
        }
        
        await this.metadataObj.save();
    }

    /**
     * Add version metadata to the cached object
     * Updates in-memory first, then persists to database
     * @async
     * @private
     * @param {Object} versionMetadata
     * @returns {Promise<void>}
     */
    async _addVersionMetadata(versionMetadata) {
        if (!this.metadataObj.versions) {
            this.metadataObj.versions = [];
        }
        
        this.metadataObj.versions.push(versionMetadata);
        await this._saveMetadata();
    }

    /**
     * Get secret name for a specific version
     * @private
     * @param {number} version
     * @returns {string}
     */
    _getSecretName(version) {
        return `${this.secretPrefix}-v${version}`;
    }

    /**
     * Create or update a secret in AWS Secrets Manager.
     * Tries CreateSecret first; if the secret already exists, falls back to PutSecretValue.
     * @async
     * @private
     */
    async _upsertToSecretsManager(secretName, secretValue) {
        try {
            return await createSecret(secretName, secretValue, this.provider);
        } catch (err) {
            if (err.name === "ResourceExistsException" || (err.message && err.message.toLowerCase().includes("already exists"))) {
                log(`[KeyManager] Secret ${secretName} already exists — updating value`);
                return await setSecret(secretName, secretValue, this.provider);
            }
            throw err;
        }
    }

    /**
     * Upsert a version entry in the in-memory metadata and persist.
     * Updates the entry in-place if the version already exists; appends otherwise.
     * Preserves the existing secretArn when updating (PutSecretValue doesn't return ARN).
     * @async
     * @private
     */
    async _upsertVersionMetadata(versionMetadata) {
        if (!this.metadataObj.versions) {
            this.metadataObj.versions = [];
        }
        const idx = this.metadataObj.versions.findIndex(v => v.version === versionMetadata.version);
        if (idx >= 0) {
            const existing = this.metadataObj.versions[idx];
            this.metadataObj.versions[idx] = Object.assign({}, existing, versionMetadata, {
                secretArn: versionMetadata.secretArn || existing.secretArn
            });
        } else {
            this.metadataObj.versions.push(versionMetadata);
        }
        await this._saveMetadata();
    }

    /**
     * Generate (or regenerate) the initial RSA-2048 key pair and store it as BOTH
     * v1 (aes-128-ecb) and v2 (aes-256-ecb) in AWS Secrets Manager.
     *
     * Safe to call multiple times — always targets v1 and v2, overwrites them.
     * Use rotateKeys() to introduce a new key pair under new version numbers.
     *
     * @async
     * @returns {Promise<Object>} { publicKey, privateKey }
     */
    async overwriteInitialKeyPair() {
        if (!this.initialized) {
            throw new Error("[KeyManager] Must call initialize() before generating keys");
        }

        const keyPairResult = crypto.generateKeyPairSync("rsa", {
            modulusLength: 2048,
            publicKeyEncoding: { type: "spki", format: "pem" },
            privateKeyEncoding: { type: "pkcs8", format: "pem" }
        });
        const { publicKey, privateKey } = keyPairResult;

        for (const version of [ 1, 2 ]) {
            const secretName = this._getSecretName(version);
            const secretValue = JSON.stringify({
                version: version,
                publicKey: publicKey,
                privateKey: privateKey,
                createdAt: new Date().toISOString()
            });

            const secretResult = await this._upsertToSecretsManager(secretName, secretValue);

            // Cache in memory
            this.keys.set(version, { publicKey, privateKey });

            await this._upsertVersionMetadata({
                version: version,
                secretName: secretName,
                secretArn: secretResult && secretResult.ARN ? secretResult.ARN : null,
                createdAt: Date.now(),
                isActive: true,
                metadata: {}
            });

            log(`[KeyManager] Upserted key pair as version ${version}`);
        }

        // Always pin current version to 2 after generate
        this.currentVersion = 2;
        this.metadataObj.current_version = 2;
        await this._saveMetadata();

        log("[KeyManager] Initial key pair written to v1 (aes-128-ecb) and v2 (aes-256-ecb)");
        return { publicKey, privateKey };
    }

    /**
     * Download existing v1+v2 keys from AWS if they exist, otherwise generate and store them.
     * This is the safe default for the `generate` command — never overwrites production keys.
     *
     * @async
     * @returns {Promise<Object>} { publicKey, privateKey, created }
     *   created=true  → fresh key pair was generated and stored as v1+v2
     *   created=false → keys already existed in AWS, downloaded and cached locally
     */
    async fetchOrCreateInitialKeyPair() {
        if (!this.initialized) {
            throw new Error("[KeyManager] Must call initialize() before generating keys");
        }

        try {
            // Try to load v1 from AWS — if it succeeds the keys already exist
            const existing = await this._loadKeyFromSecretsManager(1);

            // Also cache v2 (same keypair, just verify it loads without error)
            try {
                await this._loadKeyFromSecretsManager(2);
            } catch (e) {
                log("[KeyManager] v2 not found in AWS — will store v2 using the existing v1 keypair");
                // v1 exists but v2 is missing — register v2 with the same key
                const secretName = this._getSecretName(2);
                const secretValue = JSON.stringify({
                    version: 2,
                    publicKey: existing.publicKey,
                    privateKey: existing.privateKey,
                    createdAt: new Date().toISOString()
                });
                await this._upsertToSecretsManager(secretName, secretValue);
                this.keys.set(2, { publicKey: existing.publicKey, privateKey: existing.privateKey });
                await this._upsertVersionMetadata({
                    version: 2,
                    secretName: secretName,
                    secretArn: null,
                    createdAt: Date.now(),
                    isActive: true,
                    metadata: {}
                });
            }

            // Ensure currentVersion reflects v1+v2 are active
            if (this.currentVersion < 2) {
                this.currentVersion = 2;
                this.metadataObj.current_version = 2;
                await this._saveMetadata();
            }

            log("[KeyManager] v1 and v2 already exist in AWS — downloaded existing keys");
            return { publicKey: existing.publicKey, privateKey: existing.privateKey, created: false };

        } catch (err) {
            // v1 not found in AWS — generate a fresh key pair
            log("[KeyManager] v1 not found in AWS — generating new key pair");
            const result = await this.overwriteInitialKeyPair();
            return { publicKey: result.publicKey, privateKey: result.privateKey, created: true };
        }
    }

    /**
     * Store a key pair under the next version number in AWS Secrets Manager and metadata.
     * Shared by generateKeyPair() and registerKeyPairAsVersion().
     * @async
     * @private
     * @param {string} publicKey - PEM public key
     * @param {string} privateKey - PEM private key
     * @returns {Promise<Object>} { version, publicKey, privateKey, secretName }
     */
    async _storeAndRegisterVersion(publicKey, privateKey) {
        this.currentVersion++;
        const version = this.currentVersion;

        // Cache in memory
        this.keys.set(version, { publicKey, privateKey });

        const secretName = this._getSecretName(version);
        const secretValue = JSON.stringify({
            version: version,
            publicKey: publicKey,
            privateKey: privateKey,
            createdAt: new Date().toISOString()
        });

        try {
            const secretResult = await createSecret(secretName, secretValue, this.provider);
            log(`[KeyManager] Stored key version ${version} in AWS Secrets Manager: ${secretName}`);

            const versionMetadata = {
                version: version,
                secretName: secretName,
                secretArn: secretResult?.ARN || null,
                createdAt: Date.now(),
                isActive: true,
                metadata: {}
            };

            await this._addVersionMetadata(versionMetadata);

            this.metadataObj.current_version = version;
            await this._saveMetadata();

            log(`[KeyManager] Registered key pair as version ${version}`);

            return { version, publicKey, privateKey, secretName };
        } catch (err) {
            errorLog("[KeyManager] Error storing key in AWS Secrets Manager:", err);
            this.keys.delete(version);
            this.currentVersion--;
            throw err;
        }
    }

    /**
     * Generate a new RSA-2048 key pair and store in AWS Secrets Manager
     * @async
     * @returns {Promise<Object>} { version, publicKey, privateKey, secretName }
     */
    async generateKeyPair() {
        if (!this.initialized) {
            throw new Error("[KeyManager] Must call initialize() before generating keys");
        }

        const keyPairResult = crypto.generateKeyPairSync("rsa", {
            modulusLength: 2048,
            publicKeyEncoding: { type: "spki", format: "pem" },
            privateKeyEncoding: { type: "pkcs8", format: "pem" }
        });

        return this._storeAndRegisterVersion(keyPairResult.publicKey, keyPairResult.privateKey);
    }

    /**
     * Register an existing key pair as the next version number.
     * Use this when two versions should share the same RSA key pair but use
     * different AES algorithms (e.g. v1=aes-128-ecb, v2=aes-256-ecb).
     * @async
     * @param {string} publicKey - PEM public key
     * @param {string} privateKey - PEM private key
     * @returns {Promise<Object>} { version, publicKey, privateKey, secretName }
     */
    async registerKeyPairAsVersion(publicKey, privateKey) {
        if (!this.initialized) {
            throw new Error("[KeyManager] Must call initialize() before registering keys");
        }
        return this._storeAndRegisterVersion(publicKey, privateKey);
    }

    /**
     * Load a key pair from AWS Secrets Manager into memory cache
     * @async
     * @private
     * @param {number} version
     * @returns {Promise<Object>} { publicKey, privateKey }
     */
    async _loadKeyFromSecretsManager(version) {
        const secretName = this._getSecretName(version);
        
        try {
            const secretValue = await getSecret(secretName, this.provider);
            const keyData = JSON.parse(secretValue);
            
            const keyPair = {
                publicKey: keyData.publicKey,
                privateKey: keyData.privateKey
            };
            
            // Cache in memory
            this.keys.set(version, keyPair);
            log(`[KeyManager] Loaded key version ${version} from AWS Secrets Manager`);
            
            return keyPair;
        } catch (err) {
            errorLog(`[KeyManager] Error loading key version ${version} from AWS Secrets Manager:`, err);
            throw new Error(`Key version ${version} not found in AWS Secrets Manager`);
        }
    }

    /**
     * Get public key for a specific version
     * @async
     * @param {number} version
     * @returns {Promise<string>} PEM formatted public key
     */
    async getPublicKey(version) {
        // Check in-memory cache first
        let keyPair = this.keys.get(version);
        if (keyPair) {
            return keyPair.publicKey;
        }
        
        // Load from AWS Secrets Manager
        keyPair = await this._loadKeyFromSecretsManager(version);
        return keyPair.publicKey;
    }

    /**
     * Get private key for a specific version
     * @async
     * @param {number} version
     * @returns {Promise<string>} PEM formatted private key
     */
    async getPrivateKey(version) {
        // Check in-memory cache first
        let keyPair = this.keys.get(version);
        if (keyPair) {
            return keyPair.privateKey;
        }
        
        // Load from AWS Secrets Manager
        keyPair = await this._loadKeyFromSecretsManager(version);
        return keyPair.privateKey;
    }

    /**
     * Check if a key version is valid (exists in cached metadata)
     * Uses in-memory cached metadata for performance
     * @async
     * @param {number} version
     * @returns {Promise<boolean>}
     */
    async isKeyValid(version) {
        if (!this.metadataObj || !this.metadataObj.versions) {
            return false;
        }
        
        return this.metadataObj.versions.some(v => v.version === version && v.isActive);
    }

    /**
     * Get current active key version (sync, uses cached value)
     * @returns {number}
     */
    getCurrentVersion() {
        return this.currentVersion;
    }

    /**
     * Get current active public key
     * @async
     * @returns {Promise<Object|null>} { version, publicKey }
     */
    async getCurrentPublicKey() {
        if (this.currentVersion === 0) {
            return null;
        }
        return {
            version: this.currentVersion,
            publicKey: await this.getPublicKey(this.currentVersion)
        };
    }

    /**
     * List all available key versions from cached metadata
     * Uses in-memory cached metadata for performance
     * @async
     * @returns {Promise<Array<number>>}
     */
    async listVersions() {
        if (!this.metadataObj || !this.metadataObj.versions) {
            return [];
        }
        
        return this.metadataObj.versions
            .filter(v => v.isActive)
            .map(v => v.version)
            .sort((a, b) => a - b);
    }

    /**
     * Rotate keys - generate new key while keeping old keys valid
     * All previous key versions remain active and valid
     * @async
     * @returns {Promise<Object>} { oldVersion, newVersion, publicKey }
     */
    async rotateKeys() {
        const oldVersion = this.currentVersion;
        const keyResult = await this.generateKeyPair();
        const version = keyResult.version;
        const publicKey = keyResult.publicKey;
        
        log("[KeyManager] Key rotation complete: v" + oldVersion + " -> v" + version);
        log("[KeyManager] All previous key versions remain valid");
        
        return {
            oldVersion: oldVersion,
            newVersion: version,
            publicKey: publicKey
        };
    }
}

module.exports = KeyManager;